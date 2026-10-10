package telemetry

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync/atomic"
	"testing"
	"time"
)

func TestPostHogPayload(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != "POST" || r.URL.Path != "/prefix/i/v0/e/" {
			t.Errorf("unexpected endpoint: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("User-Agent") != "terraform-provider-telemetry/test" {
			t.Errorf("unexpected User-Agent: %q", r.Header.Get("User-Agent"))
		}
		var event map[string]any
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Error(err)
			return
		}
		if event["api_key"] != "test-token" || event["event"] != "terraform_capture" {
			t.Errorf("unexpected event: %v", event)
		}
		if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(event["distinct_id"].(string)) {
			t.Error("expected UUIDv4")
		}
		props := event["properties"].(map[string]any)
		if props["$process_person_profile"] != false || props["$geoip_disable"] != true {
			t.Error("unexpected identity settings")
		}
		if props["workspace"] != "test" {
			t.Error("missing extra data")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	CapturePostHog(context.Background(), PostHogConnection{server.URL + "/prefix/", "test-token"}, "", map[string]any{"workspace": "test"}, "test")
	if requests.Load() != 1 {
		t.Fatalf("expected one request, got %d", requests.Load())
	}
}

func TestPostHogUsesGivenDistinctID(t *testing.T) {
	var distinctID any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var event map[string]any
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Error(err)
		}
		distinctID = event["distinct_id"]
	}))
	defer server.Close()
	if err := CapturePostHog(context.Background(), PostHogConnection{server.URL, "token"}, "stable-id", nil, "test"); err != nil || distinctID != "stable-id" {
		t.Fatalf("unexpected distinct ID %v, err=%v", distinctID, err)
	}
}

func TestPostHogDoesNotRetryOrFollowRedirects(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusTemporaryRedirect} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Location", "/redirect")
				w.WriteHeader(status)
			}))
			defer server.Close()
			CapturePostHog(context.Background(), PostHogConnection{server.URL, "token"}, "", nil, "test")
			if requests.Load() != 1 {
				t.Fatalf("retried or followed redirect: %d requests", requests.Load())
			}
		})
	}
}

func TestPostHogHonorsCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(250 * time.Millisecond):
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	CapturePostHog(ctx, PostHogConnection{server.URL, "token"}, "", nil, "test")
	if time.Since(start) > time.Second {
		t.Fatal("ignored context timeout")
	}
}

func TestInvalidConnections(t *testing.T) {
	for _, connection := range []PostHogConnection{
		{"", "token"}, {"ftp://localhost", "token"}, {"https://user:secret@example.com", "token"},
		{"https://example.com?token=secret", "token"}, {"https://example.com", ""},
	} {
		if connection.Valid() {
			t.Errorf("unexpected valid connection: %v", connection.Host)
		}
		CapturePostHog(context.Background(), connection, "", nil, "test")
	}
}

func TestPostHogReportsDeliveryResult(t *testing.T) {
	for status, wantErr := range map[int]bool{
		http.StatusOK: false, http.StatusNoContent: false,
		http.StatusBadRequest: true, http.StatusInternalServerError: true, http.StatusTemporaryRedirect: true,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
		err := CapturePostHog(context.Background(), PostHogConnection{server.URL, "token"}, "", nil, "test")
		server.Close()
		if (err != nil) != wantErr {
			t.Errorf("status %d: unexpected error %v", status, err)
		}
	}
	if err := CapturePostHog(context.Background(), PostHogConnection{"", "token"}, "", nil, "test"); err == nil {
		t.Error("invalid connection did not return an error")
	}
}
