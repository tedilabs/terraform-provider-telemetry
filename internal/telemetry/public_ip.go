package telemetry

import (
	"context"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

// The external service observes the egress address, including NAT or proxies.
// No local interface addresses, event properties, or credentials are sent.
func lookupPublicIP(ctx context.Context) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api64.ipify.org", nil)
	if err != nil {
		return ""
	}
	client := &http.Client{
		Timeout:       2 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 65))
	if err != nil || len(body) > 64 {
		return ""
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(string(body)))
	if err != nil || ip.Zone() != "" {
		return ""
	}
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return ""
	}
	return ip.String()
}
