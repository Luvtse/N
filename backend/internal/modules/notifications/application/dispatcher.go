// Package application implements the notification dispatcher: a config-driven
// per-category channel policy, localized template resolution with English
// fallback, and retry bookkeeping (3 attempts, exponential backoff) for the
// async channels. In-app delivery is synchronous and never retried.
package application

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	domain "nidaw-backend/internal/modules/notifications/domain"
)

// ============================================================================
// POLICY (config-driven, not hardcoded in call sites)
// ============================================================================

// CategoryPolicy declares which channels a category fans out to and whether
// email is forced (durable-record categories: receipt, payment_failed, safety).
type CategoryPolicy struct {
	Channels   []domain.Channel
	ForceEmail bool
}

// DefaultPolicyTable is the shipped §1.4 policy. Ops can override per category
// by supplying a table built from config at module construction time.
func DefaultPolicyTable() map[domain.Category]CategoryPolicy {
	return map[domain.Category]CategoryPolicy{
		domain.CategoryRideMatched:        {Channels: []domain.Channel{domain.ChannelInApp, domain.ChannelFCM}},
		domain.CategoryDriverArriving:     {Channels: []domain.Channel{domain.ChannelInApp, domain.ChannelFCM}},
		domain.CategoryPaymentFailed:      {Channels: []domain.Channel{domain.ChannelInApp, domain.ChannelFCM}, ForceEmail: true},
		domain.CategoryPromo:              {Channels: []domain.Channel{domain.ChannelInApp, domain.ChannelFCM}},
		domain.CategorySafety:             {Channels: []domain.Channel{domain.ChannelInApp, domain.ChannelFCM}, ForceEmail: true},
		domain.CategoryReceipt:            {Channels: []domain.Channel{domain.ChannelInApp, domain.ChannelFCM}, ForceEmail: true},
		domain.CategoryScheduledReminder:  {Channels: []domain.Channel{domain.ChannelInApp, domain.ChannelFCM}},
		domain.CategoryDestinationChanged: {Channels: []domain.Channel{domain.ChannelInApp, domain.ChannelFCM}},
	}
}

// RetryConfig governs async-channel retries: MaxAttempts total tries, then
// terminal failure; delay = BaseBackoff * 2^(attempt-1).
type RetryConfig struct {
	MaxAttempts int
	BaseBackoff time.Duration
	Now         func() time.Time // injectable clock for tests
}

// DefaultRetryConfig returns the shipped policy: 3 attempts, 30s base.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{MaxAttempts: 3, BaseBackoff: 30 * time.Second, Now: time.Now}
}

func (r RetryConfig) clock() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r RetryConfig) nextDelay(attempt int) time.Duration {
	d := r.BaseBackoff
	for i := 1; i < attempt; i++ {
		d *= 2
	}
	return d
}

// ============================================================================
// ADAPTER PORTS
// ============================================================================

// WSNotifier pushes an already-persisted in-app notification to connected
// clients over the existing WebSocket hub. Implementations must be
// best-effort: an error here does not fail the notification (the row is
// durable and the inbox endpoint serves it regardless).
type WSNotifier interface {
	PushInApp(ctx context.Context, n *domain.Notification) error
}

// PushSender delivers one FCM message to one device token.
type PushSender interface {
	Send(ctx context.Context, token string, n *domain.Notification) error
}

// EmailSender delivers templated mail. bodyHTML carries the rendered HTML
// (receipt tables etc.); adapters may ignore it when unsupported.
type EmailSender interface {
	SendEmail(ctx context.Context, to string, subject string, bodyHTML string, n *domain.Notification) error
}

// ============================================================================
// TEMPLATES (en default + am/om; unknown locales fall back to en)
// ============================================================================

type tpl struct {
	Title string
	Body  string
}

// templatesByLocale maps locale -> category -> template. Interpolation uses
// {var} placeholders resolved from Command.Vars.
var templatesByLocale = map[string]map[domain.Category]tpl{
	"en": {
		domain.CategoryRideMatched:        {"Driver matched", "Your driver {driver_name} ({vehicle}) is on the way. ETA {eta_minutes} min."},
		domain.CategoryDriverArriving:     {"Driver arriving", "{driver_name} is almost at your pickup point."},
		domain.CategoryPaymentFailed:      {"Payment failed", "We couldn't process your payment of {amount} {currency}. Please update your payment method."},
		domain.CategoryPromo:              {"A promo for you", "{promo_copy}"},
		domain.CategorySafety:             {"Safety alert", "A safety event was recorded for your ride. Tap to review."},
		domain.CategoryReceipt:            {"Your receipt", "Ride complete — total {amount} {currency}. Itemized receipt inside."},
		domain.CategoryScheduledReminder:  {"Ride starting soon", "Your scheduled ride departs in about {minutes_before_pickup} minutes."},
		domain.CategoryDestinationChanged: {"Destination updated", "Your drop-off was changed to {new_destination}."},
	},
	"am": {
		domain.CategoryRideMatched:        {"አሽከርካሪ ተመድቧል", "አሽከርካሪዎ {driver_name} ({vehicle}) በመምጣት ላይ ነው። ግምት {eta_minutes} ደቂቃ።"},
		domain.CategoryDriverArriving:     {"አሽከርካሪ እየመᣣ ነው", "{driver_name} ወደ የተነሳበት ቦታዎ በጣም ቅርብ ሆነዋል።"},
		domain.CategoryPaymentFailed:      {"ክፍያ አልተሳካም", "የ{amount} {currency} ክፍያዎን መስራት አልተቻለም። እባክዎ የክፍያ ዘዴዎን ያሻሽሉ።"},
		domain.CategoryPromo:              {"ለእርስዎ ቅናሽ", "{promo_copy}"},
		domain.CategorySafety:             {"የደህንነት ማሳወቂያ", "ለጉዞዎ የደህንነት ክስተት ተመዝግቧል። ለማየት ይንኩ።"},
		domain.CategoryReceipt:            {"የክፍያ ማስረጃ", "ጉዞ ተጠናቋል — ጠቅላላ {amount} {currency}። ዝርዝር ማስረጃ ውስጥ ነው።"},
		domain.CategoryScheduledReminder:  {"ጉዞ በቅርቡ ይጀምራል", "የተያዙት ጉዞ በ{minutes_before_pickup} ደቂቃ ውስጥ ይነሳል።"},
		domain.CategoryDestinationChanged: {"መድረሻ ቦታ ተቀይሯል", "መድረሻ ቦታዎ ወደ {new_destination} ተቀይሯል።"},
	},
	"om": {
		domain.CategoryRideMatched:        {"Deemsaanni waldee", "Deemsaansi kee {driver_name} ({vehicle}) karaa irra jira. Sa'aanii {eta_minutes}."},
		domain.CategoryDriverArriving:     {"Deemsaan ni dhufaa jira", "{driver_name} bakka kaa'umsa keetiif dhiyoo jira."},
		domain.CategoryPaymentFailed:      {"Kaffalni milkaa'e", "Kaffala {amount} {currency} kee hojiirra ooluu hin dandeenye. Mallattoo kaffaltii kee haaromsi."},
		domain.CategoryPromo:              {"Qabeenyi siif", "{promo_copy}"},
		domain.CategorySafety:             {"Ibsa nageenyaa", "Taajjee nageenyaa imala keerratti galmaa'eera. Ilaaluuf tuqi."},
		domain.CategoryReceipt:            {"Raqamsa kaffaltii", "Imalli xumurame — waliigalaa {amount} {currency}. Raqamsa bal'inaan keessa jira."},
		domain.CategoryScheduledReminder:  {"Imalli yeroo gabaabaa keessatti jalqaba", "Imalli karoorfame sa'aanii {minutes_before_pickup} keessatti ka'a."},
		domain.CategoryDestinationChanged: {"Bakka geessuu jijjiirame", "Bakki geessuu kee gara {new_destination} jijjiirameera."},
	},
}

// ResolveTemplate picks the localized copy for (locale, category), falling
// back to English when the locale or category has no entry. Placeholders of
// the form {key} are replaced from vars; unresolved placeholders are left as
// empty strings so no raw braces ever reach a user-visible surface.
func ResolveTemplate(locale string, category domain.Category, vars map[string]string) (title, body string) {
	t := tpl{Title: "Notification", Body: ""}
	if byCat, ok := templatesByLocale[normalizeLocale(locale)]; ok {
		if cand, ok := byCat[category]; ok {
			t = cand
		} else if cand, ok := templatesByLocale["en"][category]; ok {
			t = cand
		}
	} else if cand, ok := templatesByLocale["en"][category]; ok {
		t = cand
	}
	return interpolate(t.Title, vars), interpolate(t.Body, vars)
}

func normalizeLocale(locale string) string {
	l := strings.ToLower(strings.TrimSpace(locale))
	if l == "" {
		return "en"
	}
	// "am-ET", "om_ ET" etc. -> primary subtag
	if i := strings.IndexAny(l, "-_"); i > 0 {
		l = l[:i]
	}
	if _, ok := templatesByLocale[l]; ok {
		return l
	}
	return "en"
}

func interpolate(s string, vars map[string]string) string {
	if len(vars) == 0 {
		// Drop any placeholders we cannot fill.
		for _, c := range []string{"{driver_name}", "{vehicle}", "{eta_minutes}", "{amount}", "{currency}", "{promo_copy}", "{minutes_before_pickup}", "{new_destination}"} {
			s = strings.ReplaceAll(s, c, "")
		}
		return s
	}
	for k, v := range vars {
		s = strings.ReplaceAll(s, "{"+k+"}", v)
	}
	// Remove leftovers not covered by vars.
	for _, key := range []string{"driver_name", "vehicle", "eta_minutes", "amount", "currency", "promo_copy", "minutes_before_pickup", "new_destination"} {
		s = strings.ReplaceAll(s, "{"+key+"}", "")
	}
	return s
}

// ============================================================================
// DISPATCHER
// ============================================================================

// Dispatcher fans a Command out across channels per the policy table.
type Dispatcher struct {
	repo     domain.Repository
	policies map[domain.Category]CategoryPolicy
	retry    RetryConfig
	inApp    WSNotifier
	push     PushSender // may be nil => fcm rows stay pending for retry worker
	email    EmailSender

	// Consumer idempotency (Phase 2): the transactional outbox guarantees
	// at-least-once bus delivery, so a redelivered event carrying the same
	// stable IdempotencyKey must not produce duplicate notification rows or
	// duplicate pushes. The window is bounded FIFO memory — safe because the
	// in-app DB row itself is always the durable source of truth for the
	// inbox; suppression only prevents redundant fan-out work.
	dedupeMu  sync.Mutex
	seenKeys  []string
	seenIndex map[string]struct{}
}

// maxDedupeKeys bounds the dispatcher's idempotency window. Ride lifecycle
// transitions are low-volume per ride, so this covers realistic broker-replay
// bursts while keeping memory small.
const maxDedupeKeys = 8192

// alreadyDispatched reports whether key was seen before, recording it and
// evicting the oldest entry when the bounded window is full. Empty keys are
// never deduped (returns false without recording).
func (d *Dispatcher) alreadyDispatched(key string) bool {
	if key == "" {
		return false
	}
	d.dedupeMu.Lock()
	defer d.dedupeMu.Unlock()
	if _, dup := d.seenIndex[key]; dup {
		return true
	}
	if len(d.seenKeys) >= maxDedupeKeys {
		oldest := d.seenKeys[0]
		d.seenKeys = d.seenKeys[1:]
		delete(d.seenIndex, oldest)
	}
	d.seenKeys = append(d.seenKeys, key)
	d.seenIndex[key] = struct{}{}
	return false
}

// NewDispatcher wires the ports. push/email may be nil in environments where
// those rails are not configured; rows are still persisted durably and the
// RetryWorker will pick them up once credentials land.
func NewDispatcher(repo domain.Repository, policies map[domain.Category]CategoryPolicy, retry RetryConfig, inApp WSNotifier, push PushSender, email EmailSender) *Dispatcher {
	if policies == nil {
		policies = DefaultPolicyTable()
	}
	return &Dispatcher{
		repo:      repo,
		policies:  policies,
		retry:     retry,
		inApp:     inApp,
		push:      push,
		email:     email,
		seenIndex: make(map[string]struct{}),
	}
}

// Dispatch persists one row per targeted channel and delivers the sync
// channel immediately. Returns the created notifications (in_app first).
func (d *Dispatcher) Dispatch(ctx context.Context, cmd domain.Command) ([]*domain.Notification, error) {
	if cmd.UserID == uuid.Nil {
		return nil, domain.ErrInvalidUser
	}
	if !cmd.Category.Valid() {
		return nil, fmt.Errorf("%w: %s", domain.ErrInvalidCategory, cmd.Category)
	}
	policy, ok := d.policies[cmd.Category]
	if !ok {
		return nil, fmt.Errorf("notification: no dispatch policy for category %q", cmd.Category)
	}

	// Consumer idempotency (Phase 2): a redelivered outbox event carries the
	// same stable key, so suppress the duplicate fan-out before any rows are
	// created. Empty keys (producers without a stable event id) fall through
	// unchanged. Returns nil/nil — callers treat "no rows created" as an
	// expected outcome, not an error.
	if d.alreadyDispatched(cmd.IdempotencyKey) {
		return nil, nil
	}

	channels := dedupeChannels(policy.Channels, policy.ForceEmail)
	var title, body string
	locale, lerr := d.repo.UserLocale(cmd.UserID)
	if lerr != nil {
		locale = "en"
	}
	title, body = ResolveTemplate(locale, cmd.Category, cmd.Vars)

	created := make([]*domain.Notification, 0, len(channels))
	now := d.retry.clock().UTC()
	for _, ch := range channels {
		n := &domain.Notification{
			ID:           uuid.New(),
			UserID:       cmd.UserID,
			Channel:      ch,
			Category:     cmd.Category,
			Title:        title,
			Body:         body,
			DeepLink:     cmd.DeepLink,
			Payload:      cmd.Payload,
			CreatedAt:    now,
			AttemptCount: 0,
			Status:       domain.StatusPending,
		}
		if err := d.repo.Create(n); err != nil {
			return created, fmt.Errorf("notification: persist %s row: %w", ch, err)
		}
		created = append(created, n)

		switch ch {
		case domain.ChannelInApp:
			d.deliverInApp(ctx, n)
		case domain.ChannelFCM:
			d.deliverFCM(ctx, n)
		case domain.ChannelEmail:
			d.deliverEmail(ctx, n)
		}
	}
	return created, nil
}

func (d *Dispatcher) deliverInApp(ctx context.Context, n *domain.Notification) {
	// In-app is synchronous and never retried: the DB row IS the delivery;
	// the WS push is a live hint only.
	at := d.retry.clock().UTC()
	_ = d.repo.MarkDelivered(n.ID, at)
	n.DeliveredAt = &at
	n.Status = domain.StatusDelivered
	if d.inApp != nil {
		// Best effort — failures here do not mark the row failed.
		_ = d.inApp.PushInApp(ctx, n)
	}
}

func (d *Dispatcher) deliverFCM(ctx context.Context, n *domain.Notification) {
	if d.push == nil {
		return // stays pending; retry worker re-checks after credential rollout
	}
	tokens, err := d.repo.ActiveTokensForUser(n.UserID)
	if err != nil || len(tokens) == 0 {
		return // no devices registered yet; row remains pending for later sends
	}
	var lastErr error
	for _, t := range tokens {
		if err := d.push.Send(ctx, t.Token, n); err != nil {
			lastErr = err
			continue
		}
	}
	if lastErr == nil {
		at := d.retry.clock().UTC()
		_ = d.repo.MarkDelivered(n.ID, at)
		return
	}
	d.recordAsyncFailure(n, lastErr.Error())
}

func (d *Dispatcher) deliverEmail(ctx context.Context, n *domain.Notification) {
	if d.email == nil {
		return
	}
	to, err := d.repo.UserEmail(n.UserID)
	if err != nil || to == "" {
		return // no email on file; skip silently (in-app copy still exists)
	}
	bodyHTML := RenderEmailHTML(n)
	if err := d.email.SendEmail(ctx, to, n.Title, bodyHTML, n); err != nil {
		d.recordAsyncFailure(n, err.Error())
		return
	}
	at := d.retry.clock().UTC()
	_ = d.repo.MarkDelivered(n.ID, at)
}

// recordAsyncFailure applies the 3-attempt exponential backoff policy.
func (d *Dispatcher) recordAsyncFailure(n *domain.Notification, reason string) {
	attempt := n.AttemptCount + 1
	now := d.retry.clock().UTC()
	if attempt >= d.retry.MaxAttempts {
		_ = d.repo.MarkFinalFailed(n.ID, reason, now)
		return
	}
	next := now.Add(d.retry.nextDelay(attempt))
	_ = d.repo.MarkAttemptFailed(n.ID, reason, attempt, &next)
}

// RetryDue processes due-for-retry rows once (called by the module worker).
func (d *Dispatcher) RetryDue(ctx context.Context) (int, error) {
	now := d.retry.clock().UTC()
	rows, err := d.repo.PendingRetries(now, 50)
	if err != nil {
		return 0, err
	}
	for _, n := range rows {
		switch n.Channel {
		case domain.ChannelFCM:
			d.deliverFCM(ctx, n)
		case domain.ChannelEmail:
			d.deliverEmail(ctx, n)
		}
	}
	return len(rows), nil
}

func dedupeChannels(channels []domain.Channel, forceEmail bool) []domain.Channel {
	seen := map[domain.Channel]bool{}
	out := make([]domain.Channel, 0, len(channels)+1)
	// In-app always ships first, always present (§1.4 priority order).
	out = append(out, domain.ChannelInApp)
	seen[domain.ChannelInApp] = true
	for _, c := range channels {
		if !c.Valid() || seen[c] {
			continue
		}
		out = append(out, c)
		seen[c] = true
	}
	if forceEmail && !seen[domain.ChannelEmail] {
		out = append(out, domain.ChannelEmail)
	}
	return out
}

// RenderEmailHTML builds the durable email copy. Receipt emails include the
// itemized breakdown carried on the payload; everything else renders the
// localized title/body plus a deep-link footer.
func RenderEmailHTML(n *domain.Notification) string {
	var sb strings.Builder
	sb.WriteString("<html><body style=\"font-family:-apple-system,Segoe UI,Roboto,sans-serif;color:#111\">")
	sb.WriteString(fmt.Sprintf("<h2>%s</h2><p>%s</p>", esc(n.Title), esc(n.Body)))
	if n.Category == domain.CategoryReceipt || n.Category == domain.CategoryPaymentFailed {
		if items, ok := n.Payload["breakdown"].([]interface{}); ok && len(items) > 0 {
			sb.WriteString("<table cellpadding=\"6\" cellspacing=\"0\" style=\"border-collapse:collapse\">")
			sb.WriteString("<tr style=\"background:#f3f3f3\"><th align=\"left\">Item</th><th align=\"right\">Amount</th></tr>")
			for _, raw := range items {
				item, ok := raw.(map[string]interface{})
				if !ok {
					continue
				}
				sb.WriteString(fmt.Sprintf("<tr><td>%s</td><td align=\"right\">%s %s</td></tr>",
					esc(stringOf(item["label"])), esc(stringOf(item["amount"])), esc(stringOf(item["currency"]))))
			}
			sb.WriteString("</table>")
		}
	}
	if n.DeepLink != "" {
		sb.WriteString(fmt.Sprintf("<p><a href=\"%s\">Open in app</a></p>", esc(n.DeepLink)))
	}
	sb.WriteString("<hr><p style=\"color:#777;font-size:12px\">Nidus — this is a durable record of an in-app notification.</p></body></html>")
	return sb.String()
}

func esc(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;")
	return r.Replace(s)
}

func stringOf(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return fmt.Sprintf("%.2f", t)
	default:
		return fmt.Sprintf("%v", t)
	}
}
