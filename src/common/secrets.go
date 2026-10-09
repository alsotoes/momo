// Package common provides shared functionality for the momo application.
package common

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"gopkg.in/ini.v1"
)

// SecretProvider is the interface for secret resolution.
// Implementations can source secrets from environment variables, files,
// HashiCorp Vault, AWS Secrets Manager, GCP Secret Manager, Azure Key Vault, etc.
type SecretProvider interface {
	// GetSecret resolves a secret by logical name.
	// Returns (value, found, error). found=false means "not in this source, try next".
	GetSecret(ctx context.Context, name string) (string, bool, error)
	// Init initializes the provider (connections, auth, caches).
	Init(ctx context.Context, cfg SecretsSourceConfig) error
	// Reload refreshes caches, re-authenticates, re-fetches secrets.
	Reload(ctx context.Context) error
	// HealthCheck returns (healthy, details) for monitoring.
	HealthCheck(ctx context.Context) (bool, string)
	// Close releases resources.
	Close() error
}

// ProviderConfig holds provider-specific configuration parsed from [secrets.<provider>].
type ProviderConfig map[string]string

// EnvProvider reads secrets from environment variables with a configurable prefix.
// Expected format: MOMO_<SECRET_NAME> (e.g., MOMO_ENCRYPTION_KEY=abc123).
type EnvProvider struct {
	prefix string
	mu     sync.RWMutex
}

// NewEnvProvider creates a new EnvProvider with the given prefix (default: "MOMO_").
func NewEnvProvider(prefix string) *EnvProvider {
	if prefix == "" {
		prefix = "MOMO_"
	}
	return &EnvProvider{prefix: prefix}
}

// Init initializes the EnvProvider (no-op for env vars).
func (e *EnvProvider) Init(ctx context.Context, cfg SecretsSourceConfig) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if cfg.EnvPrefix != "" {
		e.prefix = cfg.EnvPrefix
	}
	return nil
}

// GetSecret resolves a secret from environment variables.
// Returns (value, true, nil) if found, ("", false, nil) if not set.
func (e *EnvProvider) GetSecret(ctx context.Context, name string) (string, bool, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	envName := e.prefix + strings.ToUpper(name)
	val := os.Getenv(envName)
	if val == "" {
		return "", false, nil
	}
	return val, true, nil
}

// Reload is a no-op for environment variables (they're read fresh each time).
func (e *EnvProvider) Reload(ctx context.Context) error {
	return nil
}

// HealthCheck always returns true for environment variables.
func (e *EnvProvider) HealthCheck(ctx context.Context) (bool, string) {
	return true, "environment variables"
}

// Close is a no-op for environment variables.
func (e *EnvProvider) Close() error {
	return nil
}

// FileProvider reads secrets from a config file (e.g., momo.conf or a dedicated secrets file).
// It uses the same INI parser as the main config.
type FileProvider struct {
	path string
	cfg  *ini.File
	mu   sync.RWMutex
}

// NewFileProvider creates a new FileProvider reading from the given path.
func NewFileProvider(path string) *FileProvider {
	return &FileProvider{path: path}
}

// Init loads the config file.
func (f *FileProvider) Init(ctx context.Context, cfg SecretsSourceConfig) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if cfg.FilePath != "" {
		f.path = cfg.FilePath
	}
	if f.path == "" {
		return fmt.Errorf("file provider: path not configured")
	}
	cfgIni, err := ini.Load(f.path)
	if err != nil {
		return fmt.Errorf("failed to load secrets file %q: %w", f.path, err)
	}
	f.cfg = cfgIni
	return nil
}

// GetSecret resolves a secret from the config file.
// Secret names are mapped to config keys: "encryption_key" -> "encryption_key" in [global].
func (f *FileProvider) GetSecret(ctx context.Context, name string) (string, bool, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.cfg == nil {
		return "", false, fmt.Errorf("file provider not initialized")
	}
	// Map secret name to config key
	key := secretNameToConfigKey(name)
	sec := f.cfg.Section("global")
	if sec == nil {
		return "", false, nil
	}
	keyItem := sec.Key(key)
	if keyItem == nil || keyItem.String() == "" {
		return "", false, nil
	}
	return keyItem.String(), true, nil
}

// Reload reloads the config file.
func (f *FileProvider) Reload(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.path == "" {
		return fmt.Errorf("file provider: path not configured")
	}
	cfg, err := ini.Load(f.path)
	if err != nil {
		return fmt.Errorf("failed to reload secrets file %q: %w", f.path, err)
	}
	f.cfg = cfg
	return nil
}

// HealthCheck verifies the config file is readable.
func (f *FileProvider) HealthCheck(ctx context.Context) (bool, string) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.cfg == nil {
		return false, "not initialized"
	}
	return true, "config file: " + f.path
}

// Close is a no-op for file provider.
func (f *FileProvider) Close() error {
	return nil
}

// secretNameToConfigKey maps a logical secret name to its config key in [global].
func secretNameToConfigKey(name string) string {
	// Direct mapping for known secrets
	switch strings.ToLower(name) {
	case "encryption_key":
		return "encryption_key"
	case "auth_token":
		return "auth_token"
	case "e2ee_key":
		return "e2ee_key"
	case "e2ee_key_id":
		return "e2ee_key_id"
	case "oprf_share":
		return "oprf_share"
	default:
		// Default: use the name as-is
		return name
	}
}

// ProviderChain implements SecretProvider by trying a chain of providers in order.
// First provider that returns (value, true, nil) wins.
type ProviderChain struct {
	providers []SecretProvider
	mu        sync.RWMutex
}

// NewProviderChain creates a new ProviderChain with the given providers in order.
func NewProviderChain(providers ...SecretProvider) *ProviderChain {
	return &ProviderChain{providers: providers}
}

// Add appends a provider to the end of the chain.
func (c *ProviderChain) Add(p SecretProvider) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.providers = append(c.providers, p)
}

// Init initializes all providers in the chain.
func (c *ProviderChain) Init(ctx context.Context, cfg SecretsSourceConfig) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, p := range c.providers {
		if err := p.Init(ctx, cfg); err != nil {
			return fmt.Errorf("provider init failed: %w", err)
		}
	}
	return nil
}

// GetSecret tries each provider in order until one returns a value.
func (c *ProviderChain) GetSecret(ctx context.Context, name string) (string, bool, error) {
	c.mu.RLock()
	providers := make([]SecretProvider, len(c.providers))
	copy(providers, c.providers)
	c.mu.RUnlock()

	for _, p := range providers {
		val, found, err := p.GetSecret(ctx, name)
		if err != nil {
			return "", false, fmt.Errorf("provider error for %s: %w", name, err)
		}
		if found {
			return val, true, nil
		}
	}
	return "", false, nil
}

// Reload calls Reload on all providers.
func (c *ProviderChain) Reload(ctx context.Context) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, p := range c.providers {
		if err := p.Reload(ctx); err != nil {
			return fmt.Errorf("provider reload failed: %w", err)
		}
	}
	return nil
}

// HealthCheck checks all providers.
func (c *ProviderChain) HealthCheck(ctx context.Context) (bool, string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, p := range c.providers {
		healthy, details := p.HealthCheck(ctx)
		if !healthy {
			return false, details
		}
	}
	return true, "all providers healthy"
}

// Close closes all providers.
func (c *ProviderChain) Close() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, p := range c.providers {
		if err := p.Close(); err != nil {
			return err
		}
	}
	return nil
}

// NewProviderFromConfig creates a SecretProvider from a SecretsSourceConfig.
// Returns nil for unsupported/placeholder sources (vault, aws, gcp, azure).
func NewProviderFromConfig(source SecretsSource, cfg SecretsSourceConfig) (SecretProvider, error) {
	switch source {
	case SecretsSourceEnv:
		return NewEnvProvider(cfg.EnvPrefix), nil
	case SecretsSourceFile:
		return NewFileProvider(cfg.FilePath), nil
	case SecretsSourceVault:
		// Placeholder for Phase 2
		return nil, fmt.Errorf("vault provider not yet implemented")
	case SecretsSourceAWS:
		return nil, fmt.Errorf("aws provider not yet implemented")
	case SecretsSourceGCP:
		return nil, fmt.Errorf("gcp provider not yet implemented")
	case SecretsSourceAzure:
		return nil, fmt.Errorf("azure provider not yet implemented")
	default:
		return nil, fmt.Errorf("unknown secret source: %s", source)
	}
}

// BuildProviderChain builds a ProviderChain from a SecretsConfig.
func BuildProviderChain(cfg SecretsConfig) (*ProviderChain, error) {
	if !cfg.Enabled {
		// Disabled: return a chain that always returns not found
		return NewProviderChain(), nil
	}

	var providers []SecretProvider

	// Determine source order
	sources := cfg.Sources
	if len(sources) == 0 {
		// Default: env then file (backward compatible)
		sources = []SecretsSource{SecretsSourceEnv, SecretsSourceFile}
	}

	for _, source := range sources {
		var sourceCfg SecretsSourceConfig
		if cfg.SourceConfigs != nil {
			sourceCfg = cfg.SourceConfigs[source]
		}
		// Apply deprecated top-level fields for backward compat
		if source == SecretsSourceEnv && sourceCfg.EnvPrefix == "" {
			sourceCfg.EnvPrefix = "MOMO_"
		}
		if source == SecretsSourceFile && sourceCfg.FilePath == "" {
			sourceCfg.FilePath = "conf/momo.conf"
		}

		p, err := NewProviderFromConfig(source, sourceCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create %s provider: %w", source, err)
		}
		if p != nil {
			providers = append(providers, p)
		}
	}

	return NewProviderChain(providers...), nil
}
