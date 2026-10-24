package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeRequiresHealthyEndpoint(t *testing.T) {
	status := http.StatusOK
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
	defer s.Close()
	if err := check(s.URL); err != nil {
		t.Fatal(err)
	}
	for _, code := range []int{http.StatusServiceUnavailable, http.StatusFound} {
		status = code
		if check(s.URL) == nil {
			t.Fatalf("HTTP %d accepted", code)
		}
	}
	s.Close()
	if check(s.URL) == nil {
		t.Fatal("stopped endpoint accepted")
	}
}
