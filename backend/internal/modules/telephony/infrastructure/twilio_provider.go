package infrastructure

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	telephonydomain "nidaw-backend/internal/modules/telephony/domain"

	"github.com/google/uuid"
)

// Compile-time port check.
var _ telephonydomain.Provider = (*TwilioProxyProvider)(nil)

// TwilioAPI is the narrow slice of the Twilio Proxy REST surface this adapter
// needs. Keeping it an interface lets tests drive the provider without network
// access and lets ops swap in a FreeSWITCH/Asterisk implementation behind the
// same domain.Provider port (plan §2.2).
type TwilioAPI interface {
	CreateSession(ctx context.Context, serviceSID string, uniqueName string, ttlSeconds int) (string, error)
	CreateParticipant(ctx context.Context, serviceSID, sessionSID, identifier string) error
	DeleteSession(ctx context.Context, serviceSID, sessionSID string) error
}

// TwilioProxyProvider provisions masked-call pairings on Twilio's Proxy
// product (the short-code/number-pooling flavor, not Messaging). It requires
// TELEPHONY_TWILIO_SERVICE_SID plus credentials; when any are missing the
// constructor fails loudly at boot rather than silently disabling masking —
// a half-configured proxy leaks real numbers if clients fall back.
type TwilioProxyProvider struct {
	api        TwilioAPI
	serviceSID string
}

// NewTwilioProxyProvider builds the provider over a concrete TwilioAPI client.
func NewTwilioProxyProvider(api TwilioAPI, serviceSID string) (*TwilioProxyProvider, error) {
	if strings.TrimSpace(serviceSID) == "" {
		return nil, fmt.Errorf("telephony: Twilio Proxy service SID is required")
	}
	if api == nil {
		return nil, fmt.Errorf("telephony: Twilio API client is nil")
	}
	return &TwilioProxyProvider{
		api:        api,
		serviceSID: serviceSID,
	}, nil
}

func (p *TwilioProxyProvider) Name() string { return "twilio_proxy" }

// Provision creates one Proxy session and both participants. The session's
// TTL is derived from expiresAt so the provider self-heals even if our sweeper
// dies. On partial failure (session created, participant rejected) the session
// is deleted before returning the error — no orphaned patch tables.
func (p *TwilioProxyProvider) Provision(rideID uuid.UUID, riderNumber, driverNumber, proxyNumber string, expiresAt time.Time) (string, error) {
	ttl := int(time.Until(expiresAt).Seconds())
	if ttl < 60 {
		ttl = 60 // provider minimum; anything shorter is pointless for a ride
	}
	sessionSID, err := p.api.CreateSession(context.Background(), p.serviceSID, rideID.String(), ttl)
	if err != nil {
		return "", fmt.Errorf("twilio create session: %w", err)
	}
	for _, number := range []string{riderNumber, driverNumber} {
		if err := p.api.CreateParticipant(context.Background(), p.serviceSID, sessionSID, number); err != nil {
			// Roll back so a half-populated session never patches calls.
			_ = p.api.DeleteSession(context.Background(), p.serviceSID, sessionSID)
			return "", fmt.Errorf("twilio create participant: %w", err)
		}
	}
	return sessionSID, nil
}

func (p *TwilioProxyProvider) Teardown(providerSID string) error {
	if providerSID == "" {
		return nil
	}
	return p.api.DeleteSession(context.Background(), p.serviceSID, providerSID)
}

// HTTPRestTwilioAPI is the live implementation of TwilioAPI against Twilio's
// Proxy REST endpoints using basic auth (account SID + auth token). It is kept
// deliberately minimal — only the three calls the adapter makes.
type HTTPRestTwilioAPI struct {
	accountSID string
	authToken  string
	baseURL    string // e.g. https://proxy.twilio.org/v1 — injectable for tests
	http       *http.Client
}

func NewHTTPRestTwilioAPI(accountSID, authToken, baseURL string) (*HTTPRestTwilioAPI, error) {
	if accountSID == "" || authToken == "" {
		return nil, fmt.Errorf("telephony: twilio account SID and auth token are required")
	}
	if baseURL == "" {
		baseURL = "https://proxy.twilio.org/v1"
	}
	return &HTTPRestTwilioAPI{
		accountSID: accountSID,
		authToken:  authToken,
		baseURL:    strings.TrimRight(baseURL, "/"),
		http:       &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (a *HTTPRestTwilioAPI) CreateSession(ctx context.Context, serviceSID, uniqueName string, ttlSeconds int) (string, error) {
	form := url.Values{"UniqueName": {uniqueName}, "Ttl": {fmt.Sprintf("%d", ttlSeconds)}}
	resp, err := a.post(ctx, "/Services/"+serviceSID+"/Sessions", form.Encode())
	if err != nil {
		return "", err
	}
	sid, ok := resp["sid"].(string)
	if !ok || sid == "" {
		return "", fmt.Errorf("twilio: session response missing sid")
	}
	return sid, nil
}

func (a *HTTPRestTwilioAPI) CreateParticipant(ctx context.Context, serviceSID, sessionSID, identifier string) error {
	form := url.Values{"Identifier": {identifier}}
	_, err := a.post(ctx, "/Services/"+serviceSID+"/Sessions/"+sessionSID+"/Participants", form.Encode())
	return err
}

func (a *HTTPRestTwilioAPI) DeleteSession(ctx context.Context, serviceSID, sessionSID string) error {
	url := fmt.Sprintf("%s/Services/%s/Sessions/%s", a.baseURL, serviceSID, sessionSID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(a.accountSID, a.authToken)
	res, err := a.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return fmt.Errorf("twilio delete session: unexpected status %d", res.StatusCode)
	}
	return nil
}

// post issues a form-encoded POST with Twilio basic auth and returns the JSON
// body decoded into a generic map. Non-2xx responses become errors carrying
// the status code only — Twilio error bodies can echo participant identifiers,
// so they are never logged or wrapped into user-visible strings.
func (a *HTTPRestTwilioAPI) post(ctx context.Context, path string, form string) (map[string]interface{}, error) {
	endpoint := a.baseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(a.accountSID, a.authToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	res, err := a.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("twilio request failed: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("twilio request: unexpected status %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var out map[string]interface{}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("twilio response is not valid JSON")
	}
	return out, nil
}
