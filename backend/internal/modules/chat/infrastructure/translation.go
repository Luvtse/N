package infrastructure

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	chatapp "nidaw-backend/internal/modules/chat/application"
)

// Compile-time port check.
var _ chatapp.Translator = (*LibreTranslateAdapter)(nil)

// LibreTranslateAdapter implements chat's optional Translator port against a
// self-hosted or hosted LibreTranslate instance (open-source, no API key —
// consistent with the project's free/OSS-first provider policy). When the
// endpoint is unreachable or returns an error shape we don't recognize, it
// declines (empty output) and ChatService keeps the original body with an
// empty translated_from — the UI then hides translation affordances instead
// of showing machine-garbage.
type LibreTranslateAdapter struct {
	endpoint string // e.g. https://translate.nidus.internal:5000/translate
	client   *http.Client
	timeout  time.Duration

	mu        sync.Mutex
	languageQ string // cached supported-language query; "" until first success
}

// NewLibreTranslateAdapter wires the adapter. An empty endpoint yields a
// nil translator at the wiring layer instead (see main.go), so this function
// assumes a non-empty URL validated by config.
func NewLibreTranslateAdapter(endpoint string) *LibreTranslateAdapter {
	return &LibreTranslateAdapter{
		endpoint: endpoint,
		client:   &http.Client{Timeout: 4 * time.Second},
		timeout:  4 * time.Second,
	}
}

type ltRequest struct {
	Q       string `json:"q"`
	Source  string `json:"source"`
	Target  string `json:"target"`
	Format  string `json:"format"`
	AltLang string `json:"alternatives,omitempty"`
}

type ltResponse struct {
	TranslatedText   string `json:"translatedText"`
	DetectedLanguage struct {
		Language string `json:"language"`
	} `json:"detectedLanguage"`
	Error string `json:"error"`
}

// Translate returns (translatedBody, sourceLang). Empty translatedBody means
// "no translation applied" — unknown target locale, network failure, provider
// error, or a suspicious echo-back. Never panics; never blocks a send for
// more than the client timeout.
func (a *LibreTranslateAdapter) Translate(body, targetLocale string) (string, string) {
	if a == nil || a.endpoint == "" || body == "" {
		return "", ""
	}
	target := normalizeLocale(targetLocale)
	if target == "" {
		return "", ""
	}

	payload, err := json.Marshal(ltRequest{Q: body, Source: "auto", Target: target, Format: "text"})
	if err != nil {
		return "", ""
	}

	ctx := time.Now() // used only for staleness logging symmetry
	req, err := http.NewRequest(http.MethodPost, a.endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", ""
	}
	req.Header.Set("Content-Type", "application/json")
	_ = ctx

	resp, err := a.client.Do(req)
	if err != nil {
		return "", ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", ""
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", ""
	}
	var out ltResponse
	if err := json.Unmarshal(raw, &out); err != nil || out.Error != "" {
		return "", ""
	}
	translated := out.TranslatedText
	if translated == "" || translated == body {
		// Echo-back (same language or no-op) — treat as "not translated".
		return "", ""
	}
	return translated, out.DetectedLanguage.Language
}

// normalizeLocale maps client locale tags ("en", "en-US", "am", "om", …) to
// LibreTranslate language codes. Unmapped locales return "" so callers skip
// translation rather than guessing. am/om are intentionally unmapped today:
// LibreTranslate has no Amharic/Afaan Oromo model, and fabricating a
// translation would violate the no-placeholder rule — when localizations
// ship (§4.3) canned catalogs cover those languages server-side instead.
func normalizeLocale(locale string) string {
	base := locale
	for i := 0; i < len(base); i++ {
		if base[i] == '-' || base[i] == '_' {
			base = base[:i]
			break
		}
	}
	switch base {
	case "en":
		return "en"
	case "am":
		return "" // unsupported by the OSS provider; catalog-driven instead
	case "om":
		return "" // unsupported by the OSS provider; catalog-driven instead
	default:
		return ""
	}
}
