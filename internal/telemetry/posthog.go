package telemetry

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
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

// CapturePostHog never retries. Its error describes a failed delivery for logging;
// callers must not fail on it.
// version identifies this provider in the User-Agent header.
func CapturePostHog(ctx context.Context, connection PostHogConnection, properties map[string]any, version string) error {
	if !connection.Valid() {
		return errors.New("invalid connection")
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return err
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	distinctID := fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])

	// Property defaults can be overridden; transport fields remain separate.
	props := map[string]any{"$process_person_profile": false, "$geoip_disable": true}
	for key, value := range properties {
		props[key] = value
	}
	payload := map[string]any{
		"api_key": connection.ProjectToken, "event": "terraform_capture",
		"distinct_id": distinctID, "properties": props,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, connection.endpoint(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "terraform-provider-telemetry/"+version)
	client := &http.Client{
		Timeout: 2 * time.Second,
		// Never forward the project token through a redirect.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("unexpected response status %s", resp.Status)
	}
	return nil
}
