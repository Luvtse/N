package infrastructure

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Keyset pagination cursor shared by both MessageRepository implementations.
// The wire format is opaque to clients: base64url("<RFC3339Nano created_at>|<id>").
// Base64 keeps the colon-containing timestamp from breaking naive query-string
// handling; "|" separates because it never appears inside either component.
type chatCursor struct {
	created time.Time
	id      uuid.UUID
}

func base64Cursor(created time.Time, id uuid.UUID) string {
	raw := created.UTC().Format(time.RFC3339Nano) + "|" + id.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func parseCursor(s string) (chatCursor, error) {
	rawBytes, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return chatCursor{}, fmt.Errorf("chat: invalid cursor")
	}
	parts := strings.SplitN(string(rawBytes), "|", 2)
	if len(parts) != 2 {
		return chatCursor{}, fmt.Errorf("chat: invalid cursor")
	}
	created, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return chatCursor{}, fmt.Errorf("chat: invalid cursor")
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return chatCursor{}, fmt.Errorf("chat: invalid cursor")
	}
	return chatCursor{created: created.UTC(), id: id}, nil
}
