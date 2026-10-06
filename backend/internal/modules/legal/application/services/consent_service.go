package services

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"nidaw-backend/internal/modules/legal/domain/entities"
	"nidaw-backend/internal/shared/database"
)

var (
	ErrDocumentNotFound    = errors.New("legal document not found")
	ErrConsentRequired     = errors.New("consent required")
	ErrConsentAlreadyGiven = errors.New("consent already given")
)

type ConsentService struct {
	db *database.Postgres
}

func NewConsentService(db *database.Postgres) *ConsentService {
	return &ConsentService{db: db}
}

// GetActiveDocument retrieves the current active version of a legal document
func (s *ConsentService) GetActiveDocument(ctx context.Context, docType, language, region string) (*entities.LegalDocument, error) {
	var doc entities.LegalDocument

	query := `
		SELECT id, document_type, version, title, content, content_html, language, region, 
		       effective_date, is_active, requires_reconsent, created_at, updated_at
		FROM legal_documents
		WHERE document_type = $1 
		  AND language = $2 
		  AND (region = $3 OR region = 'GLOBAL')
		  AND is_active = true
		ORDER BY effective_date DESC
		LIMIT 1
	`

	err := s.db.QueryRow(ctx, query, docType, language, region).Scan(
		&doc.ID, &doc.DocumentType, &doc.Version, &doc.Title, &doc.Content,
		&doc.ContentHTML, &doc.Language, &doc.Region, &doc.EffectiveDate,
		&doc.IsActive, &doc.RequiresReconsent, &doc.CreatedAt, &doc.UpdatedAt,
	)

	if err != nil {
		return nil, ErrDocumentNotFound
	}

	return &doc, nil
}

// GiveConsent records user consent for a legal document
func (s *ConsentService) GiveConsent(ctx context.Context, userID uuid.UUID, req *entities.GiveConsentRequest, ipAddress, userAgent, deviceInfo string) error {
	// Get active document
	doc, err := s.GetActiveDocument(ctx, req.DocumentType, "en", "GLOBAL")
	if err != nil {
		return err
	}

	// Check if consent already exists
	var existingConsent entities.UserConsent
	err = s.db.QueryRow(ctx, `
		SELECT id, consent_given, document_version
		FROM user_consents
		WHERE user_id = $1 AND document_id = $2
	`, userID, doc.ID).Scan(&existingConsent.ID, &existingConsent.ConsentGiven, &existingConsent.DocumentVersion)

	if err == nil && existingConsent.ConsentGiven {
		// Consent already given, check if version changed
		if existingConsent.DocumentVersion == doc.Version {
			return ErrConsentAlreadyGiven
		}
		// Version changed, update consent
		return s.updateConsent(ctx, userID, doc, req, ipAddress, userAgent, deviceInfo)
	}

	// Insert new consent
	consentID := uuid.New()
	_, err = s.db.Exec(ctx, `
		INSERT INTO user_consents (id, user_id, document_id, document_type, document_version, 
		                           consent_given, consent_method, ip_address, user_agent, device_info)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, consentID, userID, doc.ID, doc.DocumentType, doc.Version, req.ConsentGiven,
		req.ConsentMethod, ipAddress, userAgent, deviceInfo)

	if err != nil {
		return err
	}

	// Log to audit trail
	s.logConsentAction(ctx, userID, doc.DocumentType, "consent_given", "", doc.Version, ipAddress, userAgent)

	return nil
}

// updateConsent updates existing consent when document version changes
func (s *ConsentService) updateConsent(ctx context.Context, userID uuid.UUID, doc *entities.LegalDocument, req *entities.GiveConsentRequest, ipAddress, userAgent, deviceInfo string) error {
	_, err := s.db.Exec(ctx, `
		UPDATE user_consents
		SET document_version = $1,
		    consent_given = $2,
		    consent_method = $3,
		    ip_address = $4,
		    user_agent = $5,
		    device_info = $6,
		    consent_timestamp = NOW()
		WHERE user_id = $7 AND document_id = $8
	`, doc.Version, req.ConsentGiven, req.ConsentMethod, ipAddress, userAgent, deviceInfo, userID, doc.ID)

	if err != nil {
		return err
	}

	// Log to audit trail
	s.logConsentAction(ctx, userID, doc.DocumentType, "consent_updated", "", doc.Version, ipAddress, userAgent)

	return nil
}

// WithdrawConsent allows user to withdraw consent
func (s *ConsentService) WithdrawConsent(ctx context.Context, userID uuid.UUID, docType, reason, ipAddress, userAgent string) error {
	_, err := s.db.Exec(ctx, `
		UPDATE user_consents
		SET consent_given = false,
		    withdrawn_at = NOW(),
		    withdrawal_reason = $1
		WHERE user_id = $2 AND document_type = $3 AND consent_given = true
	`, reason, userID, docType)

	if err != nil {
		return err
	}

	// Log to audit trail
	s.logConsentAction(ctx, userID, docType, "consent_withdrawn", "", "", ipAddress, userAgent)

	return nil
}

// GetUserConsents retrieves all consent records for a user
func (s *ConsentService) GetUserConsents(ctx context.Context, userID uuid.UUID) ([]entities.ConsentStatusResponse, error) {
	rows, err := s.db.Query(ctx, `
		SELECT uc.document_type, uc.document_version, uc.consent_given, uc.consent_timestamp,
		       ld.requires_reconsent, ld.version as latest_version
		FROM user_consents uc
		JOIN legal_documents ld ON uc.document_id = ld.id
		WHERE uc.user_id = $1
		ORDER BY uc.consent_timestamp DESC
	`, userID)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var consents []entities.ConsentStatusResponse
	for rows.Next() {
		var consent entities.ConsentStatusResponse
		var latestVersion string
		var requiresReconsent bool

		err := rows.Scan(
			&consent.DocumentType,
			&consent.DocumentVersion,
			&consent.ConsentGiven,
			&consent.ConsentDate,
			&requiresReconsent,
			&latestVersion,
		)
		if err != nil {
			return nil, err
		}

		// Check if user needs to re-consent
		consent.RequiresUpdate = requiresReconsent && consent.DocumentVersion != latestVersion

		consents = append(consents, consent)
	}

	return consents, nil
}

// CheckConsent verifies if user has given consent for a document
func (s *ConsentService) CheckConsent(ctx context.Context, userID uuid.UUID, docType string) (bool, error) {
	var consentGiven bool

	err := s.db.QueryRow(ctx, `
		SELECT consent_given
		FROM user_consents
		WHERE user_id = $1 AND document_type = $2 AND consent_given = true
	`, userID, docType).Scan(&consentGiven)

	if err != nil {
		return false, nil // No consent found
	}

	return consentGiven, nil
}

// RequireConsent is a middleware check that returns error if consent not given
func (s *ConsentService) RequireConsent(ctx context.Context, userID uuid.UUID, docType string) error {
	hasConsent, err := s.CheckConsent(ctx, userID, docType)
	if err != nil {
		return err
	}

	if !hasConsent {
		return ErrConsentRequired
	}

	return nil
}

// logConsentAction creates an immutable audit log entry
func (s *ConsentService) logConsentAction(ctx context.Context, userID uuid.UUID, docType, action, oldVersion, newVersion, ipAddress, userAgent string) {
	logID := uuid.New()

	_, err := s.db.Exec(ctx, `
		INSERT INTO consent_audit_log (id, user_id, document_type, action, old_version, new_version, ip_address, user_agent)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, logID, userID, docType, action, oldVersion, newVersion, ipAddress, userAgent)

	if err != nil {
		// Log error but don't fail the operation
		// In production, send to monitoring system
	}
}

// GetConsentAuditLog retrieves audit trail for a user (for compliance)
func (s *ConsentService) GetConsentAuditLog(ctx context.Context, userID uuid.UUID) ([]entities.ConsentAuditLog, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, user_id, document_type, action, old_version, new_version, 
		       ip_address, user_agent, metadata, created_at
		FROM consent_audit_log
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT 100
	`, userID)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []entities.ConsentAuditLog
	for rows.Next() {
		var log entities.ConsentAuditLog
		err := rows.Scan(
			&log.ID, &log.UserID, &log.DocumentType, &log.Action,
			&log.OldVersion, &log.NewVersion, &log.IPAddress,
			&log.UserAgent, &log.Metadata, &log.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}

	return logs, nil
}
