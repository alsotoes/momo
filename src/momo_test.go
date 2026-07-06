package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
	"go.uber.org/goleak"
)

const testConfig = `[global]
auth_token=super_secret_token
debug=true
auth_token=a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6 # notsecret
replication_order=3,2,1
polymorphic_system=false

[metrics]
interval=1000
min_threshold=0.1
max_threshold=0.9
fallback_interval=30

[daemon.0]
host=localhost:8080
change_replication=localhost:9090
data=/data/0
drive=/dev/sda1

[daemon.1]
host=localhost:8081
change_replication=localhost:9091
data=/data/1
drive=/dev/sdb1

[daemon.2]
host=localhost:8082
change_replication=localhost:9092
data=/data/2
drive=/dev/sdc1
`

func TestRun_Subprocess(t *testing.T) {
	tmpDir := t.TempDir()
	tmpfile := filepath.Join(tmpDir, "momo.conf")
	if err := os.WriteFile(tmpfile, []byte(testConfig), 0666); err != nil {
		t.Fatalf("Failed to write temporary config file: %v", err)
	}

	if os.Getenv("TEST_RUN_MAIN") == "1" {
		// Replace os.Args so that flag.Parse() within Run() parses our intended args
		argsStr := os.Getenv("TEST_MAIN_ARGS")
		var args []string
		if argsStr != "" {
			args = strings.Split(argsStr, " ")
		}
		os.Args = append([]string{"momo"}, args...)
		Run()
		return
	}

	tests := []struct {
		name       string
		args       string
		wantExit   bool
		wantOutput string
	}{
		{
			name:       "unknown impersonation",
			args:       "-imp unknown -config ../conf/momo.conf",
			wantExit:   true,
			wantOutput: "Option unknown",
		},
		{
			name:       "server out of range id",
			args:       "-imp server -id 999 -config ../conf/momo.conf",
			wantExit:   true,
			wantOutput: "index out of range",
		},
		{
			name:       "server negative out of range id",
			args:       "-imp server -id -2 -config ../conf/momo.conf",
			wantExit:   true,
			wantOutput: "index out of range",
		},
		{
			name:       "repl without mode",
			args:       "-imp repl -config ../conf/momo.conf",
			wantExit:   true,
			wantOutput: "Replication mode (-mode) must be specified for 'repl' impersonation",
		},
		{
			name:       "repl with out of range id",
			args:       "-imp repl -id 999 -mode 1 -config ../conf/momo.conf",
			wantExit:   true,
			wantOutput: "index out of range",
		},
		{
			name:       "invalid config path",
			args:       "-imp client -config /does/not/exist.conf",
			wantExit:   true,
			wantOutput: "Failed to get config",
		},
		{
			name:       "client with no serverId uses CRUSH but fails hash on bad file",
			args:       "-imp client -file /does/not/exist/momo -config ../conf/momo.conf",
			wantExit:   true,
			wantOutput: "Failed to hash file",
		},
		{
			name:       "client with invalid out of range server id",
			args:       "-imp client -id 999 -file /does/not/exist/momo -config ../conf/momo.conf",
			wantExit:   true,
			wantOutput: "index out of range",
		},
		{
			name:       "repl test broad cast",
			args:       "-imp repl -id -1 -mode 1 -config ../conf/momo.conf",
			wantExit:   false,
			wantOutput: "", // It might try to connect to all daemons but should eventually exit or fail connecting
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resolvedArgs := strings.ReplaceAll(tc.args, "../conf/momo.conf", tmpfile)
			cmd := exec.Command(os.Args[0], "-test.run=TestRun_Subprocess")
			cmd.Env = append(os.Environ(), "TEST_RUN_MAIN=1", "TEST_MAIN_ARGS="+resolvedArgs)
			out, err := cmd.CombinedOutput()
			outStr := string(out)

			if tc.wantExit {
				if e, ok := err.(*exec.ExitError); !ok || e.Success() {
					t.Fatalf("expected exit error, got %v, output: %s", err, outStr)
				}
				if !strings.Contains(outStr, tc.wantOutput) {
					t.Errorf("expected output to contain %q, got: %s", tc.wantOutput, outStr)
				}
			} else {
				if tc.wantOutput != "" && !strings.Contains(outStr, tc.wantOutput) {
					t.Errorf("expected output to contain %q, got: %s", tc.wantOutput, outStr)
				}
			}
		})
	}
}

func TestRunServer_Error(t *testing.T) {
	defer goleak.VerifyNone(t)

	cfg := common.Configuration{
		Daemons: []*common.Daemon{
			{Host: "127.0.0.1:0", Data: "/tmp/momo", Drive: "nvme"},
		},
		Global: common.ConfigurationGlobal{
			Protocol:          "momo-tcp",
			ReplicationFactor: 1,
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- runServer(ctx, cfg, 0)
	}()

	select {
	case <-errChan:
	case <-time.After(50 * time.Millisecond):
	}
}

func TestMain_Subprocess(t *testing.T) {
	tmpDir := t.TempDir()
	tmpfile := filepath.Join(tmpDir, "momo.conf")
	if err := os.WriteFile(tmpfile, []byte(testConfig), 0666); err != nil {
		t.Fatalf("Failed to write temporary config file: %v", err)
	}

	if os.Getenv("TEST_MAIN_FUNC") == "1" {
		configPath := os.Getenv("TEST_CONFIG_PATH")
		os.Args = []string{"momo", "-imp", "unknown", "-config", configPath}
		main()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestMain_Subprocess")
	cmd.Env = append(os.Environ(), "TEST_MAIN_FUNC=1", "TEST_CONFIG_PATH="+tmpfile)
	out, err := cmd.CombinedOutput()

	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		outStr := string(out)
		if !strings.Contains(outStr, "Option unknown") {
			t.Errorf("expected 'Option unknown', got: %s", outStr)
		}
		return
	}
	t.Fatalf("process ran with err %v, want exit error", err)
}

func TestRunServer_ContextCancel(t *testing.T) {
	defer goleak.VerifyNone(t)

	cfg := common.Configuration{
		Daemons: []*common.Daemon{
			{Host: "127.0.0.1:0", Data: "/tmp/momo", Drive: "nvme"},
		},
		Global: common.ConfigurationGlobal{
			Protocol:          "momo-tcp",
			ReplicationFactor: 1,
		},
	}
	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = runServer(ctx, cfg, 0)
	}()

	time.Sleep(10 * time.Millisecond)
	cancel()
	wg.Wait()
}

func TestRunServer_InvalidDaemonId(t *testing.T) {
	defer goleak.VerifyNone(t)

	cfg := common.Configuration{
		Daemons: []*common.Daemon{
			{Host: "127.0.0.1:0", Data: "/tmp/momo", Drive: "nvme"},
		},
		Global: common.ConfigurationGlobal{
			Protocol:          "momo-tcp",
			ReplicationFactor: 1,
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- runServer(ctx, cfg, 1)
	}()
	select {
	case <-errChan:
	case <-time.After(50 * time.Millisecond):
	}
}
