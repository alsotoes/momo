package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/alsotoes/momo/src/common"
)

func TestRunRotateSecrets_Success(t *testing.T) {
	var gotPost bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/reload-secrets" {
			gotPost = true
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	port, _ := strconv.Atoi(u.Port())

	cfg := common.Configuration{
		Daemons: []*common.Daemon{
			{MetricsBindHost: "127.0.0.1", MetricsBindPort: port},
		},
	}
	if err := runRotateSecrets(cfg, 0); err != nil {
		t.Fatalf("runRotateSecrets: %v", err)
	}
	if !gotPost {
		t.Fatal("expected POST /reload-secrets to be sent")
	}
}

func TestRunRotateSecrets_Errors(t *testing.T) {
	// Invalid server ID.
	if err := runRotateSecrets(common.Configuration{}, 5); err == nil {
		t.Fatal("expected error for out-of-range server ID")
	}

	// Server returns a non-200 status.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	port, _ := strconv.Atoi(u.Port())

	cfg := common.Configuration{
		Daemons: []*common.Daemon{
			{MetricsBindHost: "127.0.0.1", MetricsBindPort: port},
		},
	}
	if err := runRotateSecrets(cfg, 0); err == nil {
		t.Fatal("expected error for non-200 response")
	}
}
