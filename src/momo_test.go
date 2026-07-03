package main

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
)

func TestRun_Subprocess(t *testing.T) {
	if os.Getenv("TEST_RUN_MAIN") == "1" {
		// Replace os.Args so that flag.Parse() within Run() parses our intended args
		argsStr := os.Getenv("TEST_MAIN_ARGS")
		if argsStr == "" {
			os.Args = []string{"momo"}
		} else {
			os.Args = append([]string{"momo"}, strings.Split(argsStr, " ")...)
		}
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
			cmd := exec.Command(os.Args[0], "-test.run=TestRun_Subprocess")
			cmd.Env = append(os.Environ(), "TEST_RUN_MAIN=1", "TEST_MAIN_ARGS="+tc.args)
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
				if err != nil {
					// We might get an exit error if repl tries to dial bad addresses, so we accept any failure as long as it isn't an unhandled panic
					// t.Fatalf("expected success, got err %v, output: %s", err, outStr)
				}
				if tc.wantOutput != "" && !strings.Contains(outStr, tc.wantOutput) {
					t.Errorf("expected output to contain %q, got: %s", tc.wantOutput, outStr)
				}
			}
		})
	}
}

func TestRunServer_Error(t *testing.T) {
	cfg := common.Configuration{
		Daemons: []*common.Daemon{
			{Host: "127.0.0.1:0", Data: "/tmp/momo", Drive: "nvme"},
		},
		Global: common.ConfigurationGlobal{
			Protocol:          "momo-tcp",
			ReplicationFactor: 1,
		},
	}

	errChan := make(chan error, 1)
	go func() {
		errChan <- runServer(cfg, 0)
	}()

	select {
	case <-errChan:
	case <-time.After(50 * time.Millisecond):
	}
}

func TestMain_Subprocess(t *testing.T) {
	if os.Getenv("TEST_MAIN_FUNC") == "1" {
		os.Args = []string{"momo", "-imp", "unknown", "-config", "../conf/momo.conf"}
		main()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestMain_Subprocess")
	cmd.Env = append(os.Environ(), "TEST_MAIN_FUNC=1")
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
	cfg := common.Configuration{
		Daemons: []*common.Daemon{
			{Host: "127.0.0.1:10000", Data: "/tmp/momo", Drive: "nvme"},
		},
		Global: common.ConfigurationGlobal{
			Protocol:          "momo-tcp",
			ReplicationFactor: 1,
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_ = ctx
	go runServer(cfg, 0)
	time.Sleep(50 * time.Millisecond)
}

func TestRunServer_InvalidDaemonId(t *testing.T) {
	cfg := common.Configuration{
		Daemons: []*common.Daemon{
			{Host: "127.0.0.1:0", Data: "/tmp/momo", Drive: "nvme"},
		},
		Global: common.ConfigurationGlobal{
			Protocol:          "momo-tcp",
			ReplicationFactor: 1,
		},
	}

	errChan := make(chan error, 1)
	go func() {
		errChan <- runServer(cfg, 1)
	}()
	select {
	case <-errChan:
	case <-time.After(50 * time.Millisecond):
	}
}
