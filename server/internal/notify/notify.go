// Package notify delivers operator-facing webhook events (failed runs, stuck
// assignments, blocked syncs). It is intentionally dependency-free.
package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/slim/helios/server/internal/envutil"
)

// Event is the JSON payload posted to the webhook.
type Event struct {
	Event      string    `json:"event"`
	Time       time.Time `json:"time"`
	InstanceID string    `json:"instance_id,omitempty"`
	AppID      int64     `json:"app_id,omitempty"`
	AppName    string    `json:"app_name,omitempty"`
	Mode       string    `json:"mode,omitempty"`
	Status     string    `json:"status,omitempty"`
	Trigger    string    `json:"trigger,omitempty"`
	Commit     string    `json:"commit,omitempty"`
	Message    string    `json:"message,omitempty"`
}

// Notifier posts events to a webhook. A nil *Notifier is a valid disabled
// notifier, so callers never need to branch.
type Notifier struct {
	url    string
	secret string
	client *http.Client
	log    *slog.Logger
}

// FromEnv builds a notifier from HELIOS_WEBHOOK_URL / HELIOS_WEBHOOK_SECRET. It
// returns nil when no URL is configured.
func FromEnv(log *slog.Logger) *Notifier {
	url := envutil.Get("HELIOS_WEBHOOK_URL")
	if url == "" {
		return nil
	}
	timeout := 5 * time.Second
	if v := envutil.Get("HELIOS_WEBHOOK_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			timeout = d
		}
	}
	return &Notifier{
		url:    url,
		secret: envutil.Get("HELIOS_WEBHOOK_SECRET"),
		client: &http.Client{Timeout: timeout},
		log:    log,
	}
}

// Send posts the event asynchronously. Failures are logged, never retried:
// webhooks are best-effort observability, not a transactional outbox.
func (n *Notifier) Send(ev Event) {
	if n == nil {
		return
	}
	if ev.Time.IsZero() {
		ev.Time = time.Now().UTC()
	}
	go func() {
		if err := n.SendSync(context.Background(), ev); err != nil && n.log != nil {
			n.log.Warn("webhook delivery failed", "event", ev.Event, "error", err)
		}
	}()
}

// SendSync posts the event and returns any delivery error (used by tests and
// shutdown-time callers).
func (n *Notifier) SendSync(ctx context.Context, ev Event) error {
	if n == nil {
		return nil
	}
	if ev.Time.IsZero() {
		ev.Time = time.Now().UTC()
	}
	body, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Helios/1")
	if n.secret != "" {
		mac := hmac.New(sha256.New, []byte(n.secret))
		_, _ = mac.Write(body)
		req.Header.Set("X-Helios-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned %s", resp.Status)
	}
	return nil
}
