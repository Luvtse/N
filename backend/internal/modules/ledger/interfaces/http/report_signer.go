package http

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
)

// Report is the signed proof-of-balance document a driver can present to
// third parties (Phase H Step 2). It embeds the chain tip so any verifier
// with the signing key can confirm the attestation covers an exact ledger
// state — not a screenshot of a mutable row.
type Report struct {
	GeneratedAt  string     `json:"generated_at"`
	UserID       string     `json:"user_id"`
	Balance      balanceDTO `json:"balance"`
	ChainLength  int        `json:"chain_length"`
	ChainTip     string     `json:"chain_tip_hash"`
	Verified     bool       `json:"chain_verified"`
	SignatureAlg string     `json:"signature_alg,omitempty"`
	Signature    string     `json:"signature,omitempty"`
}

// CanonicalBytes returns the deterministic serialisation that gets signed:
// every field except Signature/SignatureAlg, encoded via encoding/json which
// sorts map keys and fixes struct order (fields declared in order here).
func (rep Report) CanonicalBytes() []byte {
	cp := rep
	cp.Signature = ""
	cp.SignatureAlg = ""
	b, err := json.Marshal(cp)
	if err != nil {
		// Struct is JSON-safe by construction; this cannot realistically fire.
		return []byte{}
	}
	return b
}

// ReportSigner attests audit reports.
type ReportSigner interface {
	// Algorithm names the scheme for verification tooling, e.g. "HMAC-SHA256".
	Algorithm() string
	// SignReport returns hex-encoded signature over the canonical bytes.
	SignReport(canonical []byte) (string, error)
}

// HMACReportSigner signs reports with a shared secret from LEGER_REPORT_SECRET.
// Rotate via KMS; verifiers (admin console, partner API) hold the same key.
type HMACReportSigner struct {
	secret []byte
}

// NewHMACReportSigner builds the signer; empty secrets are rejected so dev
// environments fail fast rather than shipping unsigned "signed" documents.
func NewHMACReportSigner(secret string) (*HMACReportSigner, error) {
	if len(secret) < 16 {
		return nil, errors.New("ledger: report signing secret must be at least 16 bytes")
	}
	return &HMACReportSigner{secret: []byte(secret)}, nil
}

// Algorithm implements ReportSigner.
func (s *HMACReportSigner) Algorithm() string { return "HMAC-SHA256" }

// SignReport implements ReportSigner.
func (s *HMACReportSigner) SignReport(canonical []byte) (string, error) {
	if len(canonical) == 0 {
		return "", errors.New("ledger: cannot sign empty canonical payload")
	}
	mac := hmac.New(sha256.New, s.secret)
	if _, err := mac.Write(canonical); err != nil {
		return "", err
	}
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// VerifyReport checks an HMAC signature over the report's canonical form.
// Used by the admin console and partner integrations.
func VerifyReport(secret string, rep Report) bool {
	if secret == "" || rep.SignatureAlg != "HMAC-SHA256" {
		return false
	}
	sig, err := hex.DecodeString(rep.Signature)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	if _, err := mac.Write(rep.CanonicalBytes()); err != nil {
		return false
	}
	return hmac.Equal(sig, mac.Sum(nil))
}
