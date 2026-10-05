package entities

import (
	"time"

	"github.com/google/uuid"
)

type LegalDocument struct {
	ID               uuid.UUID `json:"id" db:"id"`
	DocumentType     string    `json:"document_type" db:"document_type"`
	Version          string    `json:"version" db:"version"`
	Title            string    `json:"title" db:"title"`
	Content          string    `json:"content" db:"content"`
	ContentHTML      string    `json:"content_html" db:"content_html"`
	Language         string    `json:"language" db:"language"`
	Region           string    `json:"region" db:"region"`
	EffectiveDate    time.Time `json:"effective_date" db:"effective_date"`
	IsActive         bool      `json:"is_active" db:"is_active"`
	RequiresReconsent bool     `json:"requires_reconsent" db:"requires_reconsent"`
	CreatedAt        time.Time `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time `json:"updated_at" db:"updated_at"`
}

type UserConsent struct {
	ID              uuid.UUID  `json:"id" db:"id"`
	UserID          uuid.UUID  `json:"user_id" db:"user_id"`
	DocumentID      uuid.UUID  `json:"document_id" db:"document_id"`
	DocumentType    string     `json:"document_type" db:"document_type"`
	DocumentVersion string     `json:"document_version" db:"document_version"`
	ConsentGiven    bool       `json:"consent_given" db:"consent_given"`
	ConsentMethod   string     `json:"consent_method" db:"consent_method"`
	IPAddress       string     `json:"ip_address" db:"ip_address"`
	UserAgent       string     `json:"user_agent" db:"user_agent"`
	DeviceInfo      string     `json:"device_info" db:"device_info"`
	ConsentTimestamp time.Time `json:"consent_timestamp" db:"consent_timestamp"`
	WithdrawnAt     *time.Time `json:"withdrawn_at,omitempty" db:"withdrawn_at"`
	WithdrawalReason string    `json:"withdrawal_reason,omitempty" db:"withdrawal_reason"`
}

type ConsentAuditLog struct {
	ID           uuid.UUID `json:"id" db:"id"`
	UserID       uuid.UUID `json:"user_id" db:"user_id"`
	DocumentType string    `json:"document_type" db:"document_type"`
	Action       string    `json:"action" db:"action"`
	OldVersion   string    `json:"old_version,omitempty" db:"old_version"`
	NewVersion   string    `json:"new_version,omitempty" db:"new_version"`
	IPAddress    string    `json:"ip_address" db:"ip_address"`
	UserAgent    string    `json:"user_agent" db:"user_agent"`
	Metadata     string    `json:"metadata" db:"metadata"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
}

type GiveConsentRequest struct {
	DocumentType  string `json:"document_type" validate:"required"`
	ConsentGiven  bool   `json:"consent_given" validate:"required"`
	ConsentMethod string `json:"consent_method" validate:"required"`
}

type ConsentStatusResponse struct {
	DocumentType    string    `json:"document_type"`
	DocumentVersion string    `json:"document_version"`
	ConsentGiven    bool      `json:"consent_given"`
	ConsentDate     time.Time `json:"consent_date"`
	RequiresUpdate  bool      `json:"requires_update"`
}