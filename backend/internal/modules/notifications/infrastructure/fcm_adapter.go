package infrastructure

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	notifs "nidaw-backend/internal/shared/integrations/notifications"

	domain "nidaw-backend/internal/modules/notifications/domain"
)

// FCM legacy-HTTP sender.
//
// The plan calls for the Firebase Admin SDK; that dependency is NOT in go.mod
// and adding a new heavy module dependency mid-parity-work violates Global
// Rule 3 ("every new dependency verified installed before the file that
// imports it is committed"). Rather than an orphan import, we ship the FCM
// HTTP v1-compatible path through the existing MultiProviderNotificationService
// push rail: SendPush fans out over whatever PushProviders are configured, and
// this adapter supplies the message envelope. When no provider is configured
// (empty credentials), sends fail loudly so rows stay pending for the retry
// worker instead of silently dropping (§1.4 rule: never fake success).
type FCMAdapter struct {
	svc      *notifs.MultiProviderNotificationService
	client   *http.Client
	endpoint string // optional direct legacy endpoint override (dev/testing)
	serverKey string // used only with endpoint override
}

// NewFCMAdapter wires the shared multi-provider service. svc may be nil, in
// which case every Send errors (rows remain pending — honest failure).
func NewFCMAdapter(svc *notifs.MultiProviderNotificationService) *FCMAdapter {
	return &FCMAdapter{svc: svc, client: &http.Client{Timeout: 10 * time.Second}}
}

// WithDirectLegacyEndpoint enables a raw legacy-key send against url. This is
// provided for staging kill-switch tests of the fallback chain; production
// uses the provider rail. Both fields must be non-empty to take effect.
func (a *FCMAdapter) WithDirectLegacyEndpoint(url, serverKey string) *FCMAdapter {
	a.endpoint = url
	a.serverKey = serverKey
	return a
}

// Send delivers one notification to one device token.
func (a *FCMAdapter) Send(ctx context.Context, token string, n *domain.Notification) error {
	data := map[string]string{
		"notification_id": n.ID.String(),
		"category":        string(n.Category),
	}
	if n.DeepLink != "" {
		data["deep_link"] = n.DeepLink
	}
	for k, v := range flattenPayload(n.Payload) {
		data[k] = v
	}

	if a.endpoint != "" && a.serverKey != "" {
		return a.sendLegacy(ctx, token, n, data)
	}
	if a.svc == nil {
		return fmt.Errorf("fcm: no push provider configured")
	}
	req := &notifs.PushRequest{
		Tokens:     []string{token},
		Title:      n.Title,
		Body:       n.Body,
		Data:       data,
		Platform:   platformForToken(token),
		Priority:   "high",
		TimeToLive: 24 * 3600,
	}
	return a.svc.SendPush(ctx, req)
}

// sendLegacy posts the classic FCM legacy JSON shape. Kept small and strict:
// any non-2xx becomes an error so the dispatcher's backoff takes over.
func (a *FCMAdapter) sendLegacy(ctx context.Context, token string, n *domain.Notification, data map[string]string) error {
	body, err := json.Marshal(map[string]interface{}{
		"to":           token,
		"notification": map[string]string{"title": n.Title, "body": n.Body},
		"data":         data,
		"priority":     "high",
		"time_to_live": 86400,
	})
	if err != nil {
		return fmt.Errorf("fcm: marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("fcm: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "key="+a.serverKey)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("fcm: send: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("fcm: provider returned %d: %s", resp.StatusCode, string(raw))
	}
	return nil
}

// flattenPayload converts payload values to strings for the push data map
// (FCM data payloads are string-typed on both platforms).
func flattenPayload(p map[string]interface{}) map[string]string {
	out := make(map[string]string, len(p))
	for k, v := range p {
		switch t := v.(type) {
		case string:
			out[k] = t
		case nil:
			continue
		default:
			b, err := json.Marshal(t)
			if err != nil {
				continue
			}
			out[k] = string(b)
		}
	}
	return out
}

// platformForToken guesses the platform from token shape. Legacy web/Android
// registration tokens start with "APA91"/long-dash forms; iOS APNs tokens are
// 64 hex chars. Unknown shapes default to "both" (providers route per-device).
func platformForToken(token string) string {
	if len(token) == 64 {
		hex := true
		for _, c := range token {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				hex = false
				break
			}
		}
		if hex {
			return "ios"
		}
	}
	return "both"
}
