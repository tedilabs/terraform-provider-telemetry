package telemetry

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type ipTransport func(*http.Request) (*http.Response, error)

func (f ipTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPublicIPLookup(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	for _, test := range []struct {
		body string
		want string
	}{
		{"203.0.113.10\n", "203.0.113.10"},
		{"2001:4860:4860::8888", "2001:4860:4860::8888"},
		{"::ffff:203.0.113.10", "203.0.113.10"},
		{"10.0.0.1", ""}, {"192.168.1.1", ""}, {"127.0.0.1", ""},
		{"::1", ""}, {"fc00::1", ""}, {"fe80::1", ""}, {"fe80::1%en0", ""},
		{"0.0.0.0", ""}, {"224.0.0.1", ""}, {"::ffff:192.168.1.1", ""},
		{"<html>error</html>", ""}, {strings.Repeat(" ", 65) + "203.0.113.10", ""},
	} {
		t.Run(test.body, func(t *testing.T) {
			http.DefaultTransport = ipTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method != "GET" || r.URL.String() != "https://api64.ipify.org" || r.Body != nil || len(r.Header) != 0 {
					t.Fatalf("unexpected lookup request: %v", r)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(test.body))}, nil
			})
			if got := lookupPublicIP(context.Background()); got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestPublicIPFailureAndCancellation(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	for _, status := range []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusFound} {
		calls := 0
		http.DefaultTransport = ipTransport(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: status, Header: http.Header{"Location": {"https://example.invalid"}}, Body: io.NopCloser(strings.NewReader("203.0.113.10"))}, nil
		})
		if got := lookupPublicIP(context.Background()); got != "" || calls != 1 {
			t.Fatalf("status %d: result %q, calls %d", status, got, calls)
		}
	}
	http.DefaultTransport = ipTransport(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline") })
	if lookupPublicIP(context.Background()) != "" {
		t.Fatal("offline lookup returned an IP")
	}
	http.DefaultTransport = ipTransport(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if lookupPublicIP(ctx) != "" {
		t.Fatal("cancelled lookup returned an IP")
	}
}

func TestNetworkPublicIPCacheAndFailure(t *testing.T) {
	for _, result := range []string{"203.0.113.10", ""} {
		c := NewCollector()
		var calls atomic.Int32
		c.LookupPublicIP = func(context.Context) string { calls.Add(1); return result }
		var wg sync.WaitGroup
		for range 100 {
			wg.Go(func() {
				got := c.Collect(context.Background(), Options{Network: true, Machine: true}, true)
				network := got["network"].(map[string]any)
				ip, exists := network["public_ip"]
				if (result == "" && exists) || (result != "" && ip != result) || got["machine"] == nil {
					t.Errorf("unexpected metadata: %v", got)
				}
				if _, exists := network["ips"]; exists {
					t.Error("local interface addresses were collected")
				}
			})
		}
		wg.Wait()
		if calls.Load() != 1 {
			t.Fatalf("expected one lookup, got %d", calls.Load())
		}
		c.Collect(context.Background(), Options{Network: true}, false)
		if calls.Load() != 2 {
			t.Fatal("cache bypass did not repeat lookup")
		}
	}
}
