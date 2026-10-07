// fraud_ml_client.go — Phase G Step 1: optional ML fraud-scoring client.
//
// Roadmap: "Call ml/fraud-detection endpoint (if available) or rule-based
// fallback". This client implements services.MLScoreProvider against the
// internal ML service over plain HTTP with a shared bearer token. ANY failure
// (unreachable, non-200, unparseable body, timeout) returns an error and the
// FraudDetectionService degrades to the rule stack — the model can only ever
// ADD suspicion, never remove it.
package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// MLClientConfig points at the ml/fraud-detection deployment.
type MLClientConfig struct {
	BaseURL string        // e.g. "http://ml-fraud-detection:8080"
	Path    string        // default "/fraud-detection/score"
	Token   string        // optional bearer token (env-driven, never committed)
	Timeout time.Duration // default 2s — must not stall the withdrawal path
}

func (c MLClientConfig) withDefaults() MLClientConfig {
	if c.Path == "" {
		c.Path = "/fraud-detection/score"
	}
	if c.Timeout <= 0 {
		c.Timeout = 2 * time.Second
	}
	return c
}

// MLClient is a concrete MLScoreProvider. Construct via NewMLClient; a nil
// return means "not configured" and callers should pass nil into the fraud
// service (which then runs rules-only).
type MLClient struct {
	cfg  MLClientConfig
	http *http.Client
}

// NewMLClient builds the client; returns (nil, nil) when BaseURL is empty so
// wiring code can treat "unset" as a normal configuration state.
func NewMLClient(cfg MLClientConfig) (*MLClient, error) {
	if cfg.BaseURL == "" {
		return nil, nil
	}
	cfg = cfg.withDefaults()
	return &MLClient{
		cfg:  cfg,
		http: &http.Client{Timeout: cfg.Timeout},
	}, nil
}

type mlScoreRequest struct {
	UserID      string `json:"user_id"`
	AmountCents int64  `json:"amount_cents"`
	Currency    string `json:"currency"`
	Feature     string `json:"feature"`
}

type mlScoreResponse struct {
	Score float64 `json:"score"` // 0..1 probability of fraud
}

// ScoreWithdrawal implements MLScoreProvider.
func (c *MLClient) ScoreWithdrawal(ctx context.Context, userID uuid.UUID, amountCents int64) (float64, error) {
	if c == nil {
		return 0, errors.New("ledger/fraud: ml client not configured")
	}
	if userID == uuid.Nil {
		return 0, errors.New("ledger/fraud: ml score requires user id")
	}
	body, err := json.Marshal(mlScoreRequest{
		UserID:      userID.String(),
		AmountCents: amountCents,
		Currency:    "ETB",
		Feature:     "withdrawal",
	})
	if err != nil {
		return 0, fmt.Errorf("ledger/fraud: ml request encode: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+c.cfg.Path, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("ledger/fraud: ml request build: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("ledger/fraud: ml call: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		return 0, fmt.Errorf("ledger/fraud: ml status %d", resp.StatusCode)
	}

	var out mlScoreResponse
	dec := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	if err := dec.Decode(&out); err != nil {
		return 0, fmt.Errorf("ledger/fraud: ml response decode: %w", err)
	}
	if out.Score < 0 || out.Score > 1 {
		return 0, fmt.Errorf("ledger/fraud: ml score out of range: %v", out.Score)
	}
	return out.Score, nil
}
