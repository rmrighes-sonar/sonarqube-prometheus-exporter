package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestGetenv(t *testing.T) {
	t.Run("returns the env var when set", func(t *testing.T) {
		t.Setenv("SONARQUBE_EXPORTER_TEST_VAR", "custom-value")
		if got := getenv("SONARQUBE_EXPORTER_TEST_VAR", "fallback"); got != "custom-value" {
			t.Errorf("getenv() = %q, want %q", got, "custom-value")
		}
	})

	t.Run("returns the fallback when unset", func(t *testing.T) {
		_ = os.Unsetenv("SONARQUBE_EXPORTER_TEST_VAR_UNSET")
		if got := getenv("SONARQUBE_EXPORTER_TEST_VAR_UNSET", "fallback"); got != "fallback" {
			t.Errorf("getenv() = %q, want %q", got, "fallback")
		}
	})

	t.Run("returns the fallback when set to empty string", func(t *testing.T) {
		t.Setenv("SONARQUBE_EXPORTER_TEST_VAR_EMPTY", "")
		if got := getenv("SONARQUBE_EXPORTER_TEST_VAR_EMPTY", "fallback"); got != "fallback" {
			t.Errorf("getenv() = %q, want %q", got, "fallback")
		}
	})
}

func TestNewMux(t *testing.T) {
	client := NewSonarQubeClient("http://unused.invalid", "")
	mux := newMux(NewCollector(client))

	cases := []struct {
		path       string
		wantStatus int
		wantBody   string // substring; empty means "don't check"
	}{
		{"/healthz", http.StatusOK, "ok"},
		{"/", http.StatusOK, "sonarqube-exporter"},
		{"/metrics", http.StatusOK, ""}, // exact body depends on registered collectors; just check it serves
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Errorf("GET %s status = %d, want %d", tc.path, rec.Code, tc.wantStatus)
			}
			if tc.wantBody != "" && !strings.Contains(rec.Body.String(), tc.wantBody) {
				t.Errorf("GET %s body = %q, want it to contain %q", tc.path, rec.Body.String(), tc.wantBody)
			}
		})
	}
}
