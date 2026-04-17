package share

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientHealthChecksAdminAndPublic(t *testing.T) {
	t.Parallel()

	publicServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer publicServer.Close()

	adminServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"ok":true,"public_base_url":"%s"}`, publicServer.URL)
	}))
	defer adminServer.Close()

	client := NewClient(adminServer.URL)
	if err := client.Health(); err != nil {
		t.Fatalf("Health: %v", err)
	}
}

func TestClientHealthFailsWhenPublicHealthFails(t *testing.T) {
	t.Parallel()

	publicServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "broken", http.StatusInternalServerError)
	}))
	defer publicServer.Close()

	adminServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"ok":true,"public_base_url":"%s"}`, publicServer.URL)
	}))
	defer adminServer.Close()

	client := NewClient(adminServer.URL)
	err := client.Health()
	if err == nil {
		t.Fatal("expected Health to fail when public health is not ok")
	}
	if !strings.Contains(err.Error(), "public health status") {
		t.Fatalf("expected public health error, got: %v", err)
	}
}

func TestClientHealthFailsWhenPublicURLMissing(t *testing.T) {
	t.Parallel()

	adminServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer adminServer.Close()

	client := NewClient(adminServer.URL)
	err := client.Health()
	if err == nil {
		t.Fatal("expected Health to fail when public_base_url is missing")
	}
	if !strings.Contains(err.Error(), "missing public base url") {
		t.Fatalf("unexpected error: %v", err)
	}
}
