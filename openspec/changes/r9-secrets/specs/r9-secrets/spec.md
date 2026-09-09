> GitHub Issue URL: https://github.com/alsotoes/momo/issues/937

# Spec: R9 — Secrets Management + Key Rotation

## Requirements

### R9-S1: External Secret Sources

**Requirement:** Secrets MUST be loadable from configurable external sources.

**Scenario: Load from environment variables**

Given `[secrets] source = "env"` and `MOMO_ENCRYPTION_KEY=abc123`,
When the server starts,
Then the encryption key is loaded from the environment variable.

**Scenario: Load from HashiCorp Vault**

Given `[secrets] source = "vault"` and `[secrets.vault] address = "http://vault:8200"`,
When the server starts,
Then the secret is fetched from Vault at the configured path.

**Scenario: Multiple source fallback**

Given `[secrets] sources = ["env", "vault", "file"]`,
When loading a secret,
Then the first source that returns a value is used (in order).

### R9-S2: Versioned Key Store

**Requirement:** All secrets MUST be versioned in a BoltDB key registry.

**Scenario: Key registry stores versions**

Given a new encryption key is generated,
When stored in the key registry,
Then it receives a unique `key_id`, version number, `created_at`, `algorithm`, and `status = "active"`.

**Scenario: Retired keys retained**

Given a key is rotated,
When the old key is marked `retired`,
Then it remains in the registry for decryption of existing data,
And is never deleted.

**Scenario: Active key per purpose**

Given multiple keys for `encryption` purpose,
When `GetActiveKey("encryption")` is called,
Then it returns the single key with `status = "active"`.

### R9-S3: Automated Rotation

**Requirement:** Keys MUST be rotatable on schedule and on demand.

**Scenario: Scheduled rotation**

Given `[secrets] rotation_interval = "90d"`,
When the interval elapses,
Then the rotation flow executes for all active keys.

**Scenario: Manual rotation**

Given `momo rotate-secrets --purpose auth`,
When executed,
Then a new auth token is generated, stored as new version, and hot-reloaded.

**Scenario: Graceful reload**

Given a new key version is activated,
When nodes receive SIGHUP or call `/reload-secrets`,
Then they atomically switch to the new active key for new operations,
And retain the old key for in-flight decryption.

**Scenario: Grace period**

Given `[secrets] rotation_grace_period = "24h"`,
When a key is rotated,
Then the old key remains `active` for decryption for 24h,
Then transitions to `retired`.

### R9-S4: Rotation Audit Trail

**Requirement:** Every rotation MUST produce an audit entry.

**Scenario: Rotation logged**

Given a rotation completes (scheduled or manual),
When the new key is activated,
Then the audit log contains an entry with:
- `ts`, `purpose`, `old_key_id`, `new_key_id`, `operator`, `trigger` (scheduled/manual), `success: true`

**Scenario: Failed rotation logged**

Given a rotation fails (e.g., Vault unreachable),
When the failure is detected,
Then the audit log contains an entry with `success: false`, `error` message, and `retry_count`.

## Non-Goals

- HSM integration
- Shamir secret sharing
- Automatic revocation of compromised keys
- Cross-region secret sync