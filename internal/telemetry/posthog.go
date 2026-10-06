package telemetry

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type PostHogConnection struct {
	Host         string `tfsdk:"host"`
	ProjectToken string `tfsdk:"project_token"`
}

func (c PostHogConnection) endpoint() string {
	u, err := url.Parse(c.Host)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return ""
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/i/v0/e/"
	u.RawPath = ""
	return u.String()
}

func (c PostHogConnection) Valid() bool {
	return c.endpoint() != "" && strings.TrimSpace(c.ProjectToken) != ""
}

// CapturePostHog deliberately ignores all transport failures and never retries.
func CapturePostHog(ctx context.Context, connection PostHogConnection, properties map[string]any) {
	if !connection.Valid() {
		return
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	distinctID := fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])

	// Keep event properties separate from transport and identity controls.
	props := make(map[string]any, len(properties)+2)
	for key, value := range properties {
		props[key] = value
	}
	props["$process_person_profile"] = false
	props["$geoip_disable"] = true
	payload := map[string]any{
		"api_key": connection.ProjectToken, "event": "terraform_capture",
		"distinct_id": distinctID, "properties": props,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, connection.endpoint(), bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{
		Timeout: 2 * time.Second,
		// Never forward the project token through a redirect.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}
