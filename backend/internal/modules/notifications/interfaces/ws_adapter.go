// Package interfaces exposes the notifications module to the outside world:
// a WebSocket in-app adapter (this file), HTTP handlers, and the event-bus
// consumer that turns ride/payment domain events into notification Commands.
package interfaces

import (
	"context"
	"sync"

	domain "nidaw-backend/internal/modules/notifications/domain"
)

// InAppBroadcaster is the minimal surface the notifications module needs from
// the nidus WebSocket hub. *handlers.WebSocketHandler satisfies it via the
// thin shim wired in cmd/server/main.go (the hub broadcasts by topic, so we
// adapt per-user pushes through this registry).
type InAppBroadcaster interface {
	BroadcastToTopic(topic string, payload map[string]interface{}) error
}

// UserSocketRegistry tracks which connected user IDs have live sockets and
// forwards per-user in-app pushes to them. The main.go shim registers the WS
// hub's broadcast channel behind BroadcastToTopic; the registry keeps the
// unread-count invalidation cheap for handlers.
type UserSocketRegistry struct {
	mu      sync.RWMutex
	online  map[string]bool // userID -> has at least one live socket
}

// NewUserSocketRegistry returns an empty registry.
func NewUserSocketRegistry() *UserSocketRegistry {
	return &UserSocketRegistry{online: make(map[string]bool)}
}

// MarkOnline records that userID currently holds a live socket. Called by the
// WS hub's register path through the main.go shim (best-effort; correctness of
// delivery never depends on this hint — the DB row IS the delivery).
func (r *UserSocketRegistry) MarkOnline(userID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.online[userID] = true
}

// MarkOffline drops the hint when a user's last socket closes.
func (r *UserSocketRegistry) MarkOffline(userID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.online, userID)
}

// IsOnline reports whether we believe the user has a live socket.
func (r *UserSocketRegistry) IsOnline(userID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.online[userID]
}

// WSInAppAdapter implements application.WSNotifier on top of an
// InAppBroadcaster. Pushes are best-effort: any transport error is swallowed
// because the persisted in_app row already constitutes delivery (§1.4 rule:
// "in-app is synchronous and never retried"; the WS frame is a live hint).
type WSInAppAdapter struct {
	broadcaster InAppBroadcaster
	registry    *UserSocketRegistry
}

// NewWSInAppAdapter wires the adapter. broadcaster may be nil (single-binary
// dev mode without WS); pushes then become no-ops while inbox still works.
func NewWSInAppAdapter(broadcaster InAppBroadcaster, registry *UserSocketRegistry) *WSInAppAdapter {
	return &WSInAppAdapter{broadcaster: broadcaster, registry: registry}
}

// PushInApp sends the live hint on topic "inbox:<user_id>". Clients subscribe
// to their own inbox topic over the same socket they use for ride updates.
func (a *WSInAppAdapter) PushInApp(ctx context.Context, n *domain.Notification) error {
	if a.broadcaster == nil {
		return nil
	}
	payload := map[string]interface{}{
		"type":    "notification_inbox",
		"topic":   "inbox:" + n.UserID.String(),
		"id":      n.ID.String(),
		"user_id": n.UserID.String(),
		"channel": string(n.Channel),
		"category": string(n.Category),
		"title":   n.Title,
		"body":    n.Body,
		"payload": n.Payload,
	}
	if n.DeepLink != "" {
		payload["deep_link"] = n.DeepLink
	}
	if err := a.broadcaster.BroadcastToTopic("inbox:"+n.UserID.String(), payload); err != nil {
		// Best effort by contract — never fail the notification on a hint.
		return nil
	}
	if a.registry != nil {
		a.registry.MarkOnline(n.UserID.String())
	}
	return nil
}
