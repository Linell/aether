package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewRejectsEmptyToken(t *testing.T) {
	if _, err := New(nil, ""); err == nil {
		t.Fatal("New with empty token: want error, got nil")
	}
}

func TestAuthFailsClosed(t *testing.T) {
	h, err := New(nil, "secret")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cases := []struct {
		name, path, auth string
		wantUnauthorized bool
	}{
		{"healthz without token", "/healthz", "", false},
		{"missing token", "/v1/whatever", "", true},
		{"wrong token", "/v1/whatever", "Bearer wrong", true},
		{"correct token", "/v1/whatever", "Bearer secret", false},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		if tc.auth != "" {
			req.Header.Set("Authorization", tc.auth)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if got := rec.Code == http.StatusUnauthorized; got != tc.wantUnauthorized {
			t.Errorf("%s: status = %d", tc.name, rec.Code)
		}
	}
}
