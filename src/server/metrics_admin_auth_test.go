package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestIsLoopbackHost(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1":    true,
		"::1":          true,
		"[::1]":        true,
		"localhost":    true,
		"LOCALHOST":    true,
		"0.0.0.0":      false,
		"::":           false,
		"":             false,
		"192.168.1.10": false,
		"example.com":  false,
	}
	for host, want := range cases {
		if got := isLoopbackHost(host); got != want {
			t.Errorf("isLoopbackHost(%q) = %v, want %v", host, got, want)
		}
	}
}

func bearerReq(token string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/reload-secrets", nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	return r
}

func TestBearerAuthorized(t *testing.T) {
	const token = "0123456789abcdef" // notsecret
	if !bearerAuthorized(bearerReq(token), token) {
		t.Fatal("correct token must authorize")
	}
	if bearerAuthorized(bearerReq("0123456789abcdeX"), token) {
		t.Fatal("wrong token same length must not authorize")
	}
	if bearerAuthorized(bearerReq(""), token) {
		t.Fatal("missing token must not authorize")
	}
	if bearerAuthorized(bearerReq("short"), token) {
		t.Fatal("shorter token must not authorize")
	}
	r := httptest.NewRequest(http.MethodPost, "/x", nil)
	r.Header.Set("Authorization", token) // no Bearer prefix
	if bearerAuthorized(r, token) {
		t.Fatal("missing Bearer prefix must not authorize")
	}
	r.Header.Set("Authorization", "bearer "+token) // case-insensitive scheme
	if !bearerAuthorized(r, token) {
		t.Fatal("scheme comparison must be case-insensitive")
	}
}

func TestRequireAdminGate(t *testing.T) {
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
	const token = "0123456789abcdef" // notsecret

	// No token configured: read endpoints stay open (backward compatible).
	rec := httptest.NewRecorder()
	requireAdmin(ok, "")(rec, bearerReq(""))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("open mode status = %d", rec.Code)
	}
	// Token configured: missing and wrong are 401, correct passes.
	rec = httptest.NewRecorder()
	requireAdmin(ok, token)(rec, bearerReq(""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing token status = %d, want 401", rec.Code)
	}
	rec = httptest.NewRecorder()
	requireAdmin(ok, token)(rec, bearerReq("wrongwrongwrong!"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token status = %d, want 401", rec.Code)
	}
	rec = httptest.NewRecorder()
	requireAdmin(ok, token)(rec, bearerReq(token))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("correct token status = %d", rec.Code)
	}
}

func TestRequireMutationAuthGate(t *testing.T) {
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
	const token = "0123456789abcdef" // notsecret

	cases := []struct {
		name  string
		token string
		host  string
		req   *http.Request
		want  int
	}{
		{"loopback no token", "", "127.0.0.1", bearerReq(""), http.StatusNoContent},
		{"loopback v6 no token", "", "::1", bearerReq(""), http.StatusNoContent},
		{"non-loopback no token", "", "0.0.0.0", bearerReq(""), http.StatusForbidden},
		{"routable no token", "", "10.0.0.5", bearerReq(""), http.StatusForbidden},
		{"token correct on wildcard", token, "0.0.0.0", bearerReq(token), http.StatusNoContent},
		{"token wrong on wildcard", token, "0.0.0.0", bearerReq("wrongwrongwrong!"), http.StatusUnauthorized},
		{"token missing on wildcard", token, "0.0.0.0", bearerReq(""), http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			requireMutationAuth(ok, tc.token, tc.host)(rec, tc.req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

func TestAdminListenerRefusesUnauthenticatedMutationOffLoopback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	port := freePort(t)
	reloaded := make(chan struct{}, 1)
	reloadFn := func() {
		select {
		case reloaded <- struct{}{}:
		default:
		}
	}
	done := make(chan struct{})
	go func() { StartMetricsServer(ctx, "0.0.0.0", port, NewMetricsCollector(), reloadFn, ""); close(done) }()
	httpGetPort(t, port, "/health")

	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/reload-secrets"
	resp, err := http.Post(url, "text/plain", nil)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 on a non-loopback bind without a token", resp.StatusCode)
	}
	select {
	case <-reloaded:
		t.Fatal("an unauthenticated request must not reload secrets")
	case <-time.After(100 * time.Millisecond):
	}

	cancel()
	<-done
}

func TestAdminListenerTokenProtectsReadsAndMutation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const token = "0123456789abcdef" // notsecret
	port := freePort(t)
	reloaded := make(chan struct{}, 1)
	reloadFn := func() {
		select {
		case reloaded <- struct{}{}:
		default:
		}
	}
	done := make(chan struct{})
	go func() { StartMetricsServer(ctx, "0.0.0.0", port, NewMetricsCollector(), reloadFn, token); close(done) }()
	httpGetPort(t, port, "/health") // /health stays open

	// Read endpoint without a token is now 401.
	if code, _ := httpGetPort(t, port, "/metrics"); code != http.StatusUnauthorized {
		t.Fatalf("/metrics status = %d, want 401 when a token is configured", code)
	}
	// /health is still open.
	if code, _ := httpGetPort(t, port, "/health"); code != http.StatusOK {
		t.Fatalf("/health status = %d, want 200", code)
	}

	do := func(auth string) *http.Response {
		req, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:"+strconv.Itoa(port)+"/reload-secrets", nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST: %v", err)
		}
		return resp
	}
	resp := do("Bearer wrongwrongwrong!")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token status = %d, want 401", resp.StatusCode)
	}
	resp = do("Bearer " + token)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("correct token status = %d, want 200", resp.StatusCode)
	}
	select {
	case <-reloaded:
	case <-time.After(time.Second):
		t.Fatal("authenticated reload did not run")
	}

	cancel()
	<-done
}
