package infrastructure

import (
	"context"
	"fmt"
	"strings"

	notifs "nidaw-backend/internal/shared/integrations/notifications"

	domain "nidaw-backend/internal/modules/notifications/domain"
)

// EmailAdapter implements application.EmailSender on top of the shared
// MultiProviderNotificationService email rail (SendGrid primary, AWS SES
// fallback — see internal/shared/integrations/notifications/service.go).
// The plan's "existing transactional email provider if one exists" condition
// is satisfied by that rail, so no new SMTP dependency is introduced.
type EmailAdapter struct {
	svc      *notifs.MultiProviderNotificationService
	fromAddr string
}

// NewEmailAdapter wires the rail. fromAddr is the transactional sender
// ("Nidus <receipts@nidaw.com>"); empty falls back to a documented default so
// misconfiguration produces a provider-side rejection (loud failure), not a
// silent drop.
func NewEmailAdapter(svc *notifs.MultiProviderNotificationService, fromAddr string) *EmailAdapter {
	if fromAddr == "" {
		fromAddr = "notifications@nidaw.com"
	}
	return &EmailAdapter{svc: svc, fromAddr: fromAddr}
}

// SendEmail delivers the templated HTML body. n carries category/payload for
// providers that support structured templates; we always send rendered HTML so
// the durable record is self-contained (§1.4: receipts itemized in email).
func (a *EmailAdapter) SendEmail(ctx context.Context, to string, subject string, bodyHTML string, n *domain.Notification) error {
	if a.svc == nil {
		return fmt.Errorf("email: no email provider configured")
	}
	if strings.TrimSpace(to) == "" {
		return fmt.Errorf("email: empty recipient")
	}
	req := &notifs.EmailRequest{
		To:       []string{to},
		From:     a.fromAddr,
		ReplyTo:  a.fromAddr,
		Subject:  subject,
		HTMLBody: bodyHTML,
		TextBody: plainTextFallback(n),
		Vars: map[string]string{
			"category":        string(n.Category),
			"notification_id": n.ID.String(),
		},
	}
	return a.svc.SendEmail(ctx, req)
}

// plainTextFallback renders the text/plain alternative of the HTML copy.
func plainTextFallback(n *domain.Notification) string {
	var sb strings.Builder
	sb.WriteString(n.Title + "\n\n" + n.Body + "\n")
	if n.DeepLink != "" {
		sb.WriteString("\nOpen in app: " + n.DeepLink + "\n")
	}
	sb.WriteString("\n-- Nidus durable record\n")
	return sb.String()
}
