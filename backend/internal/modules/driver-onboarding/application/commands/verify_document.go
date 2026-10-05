package commands

import (
	"context"
	"errors"
	"fmt"
	"time"

	"nidaw-backend/internal/shared/database"
	"nidaw-backend/internal/shared/eventbus"

	"github.com/google/uuid"
)

// ============================================================================
// ERRORS
// ============================================================================

var (
	ErrDocumentNotFound     = errors.New("document not found")
	ErrInvalidDocumentType  = errors.New("invalid document type")
	ErrDocumentExpired      = errors.New("document has expired")
	ErrVerificationFailed   = errors.New("document verification failed")
	ErrDocumentAlreadyVerified = errors.New("document already verified")
)

// ============================================================================
// TYPES
// ============================================================================

// DocumentType represents the type of driver document
type DocumentType string

const (
	DocumentTypeDriversLicense    DocumentType = "drivers_license"
	DocumentTypeVehicleRegistration DocumentType = "vehicle_registration"
	DocumentTypeInsurance         DocumentType = "insurance"
	DocumentTypeBackgroundCheck   DocumentType = "background_check"
	DocumentTypeVehicleInspection DocumentType = "vehicle_inspection"
)

// DocumentStatus represents the verification status
type DocumentStatus string

const (
	DocumentStatusPending   DocumentStatus = "pending"
	DocumentStatusVerified  DocumentStatus = "verified"
	DocumentStatusRejected  DocumentStatus = "rejected"
	DocumentStatusExpired   DocumentStatus = "expired"
)

// VerificationResult contains the result of document verification
type VerificationResult struct {
	DocumentID      uuid.UUID      `json:"document_id"`
	Status          DocumentStatus `json:"status"`
	VerificationScore float64      `json:"verification_score"` // 0-1
	RejectionReason string         `json:"rejection_reason,omitempty"`
	VerifiedAt      *time.Time     `json:"verified_at,omitempty"`
	ExpiresAt       *time.Time     `json:"expires_at,omitempty"`
}

// ============================================================================
// COMMAND
// ============================================================================

// VerifyDocumentCommand represents a document verification request
type VerifyDocumentCommand struct {
	DocumentID      uuid.UUID
	DocumentType    DocumentType
	DocumentNumber  string
	IssueDate       time.Time
	ExpiryDate      time.Time
	FileURL         string
	DriverID        uuid.UUID
	VerifiedBy      uuid.UUID // Admin or automated system
}

// ============================================================================
// HANDLER
// ============================================================================

// VerifyDocumentHandler handles document verification
type VerifyDocumentHandler struct {
	db                  *database.Postgres
	eventBus            eventbus.EventBus
	verificationService DocumentVerificationService
}

// DocumentVerificationService interface for external verification
type DocumentVerificationService interface {
	VerifyDocument(ctx context.Context, req *VerificationRequest) (*VerificationResponse, error)
}

// VerificationRequest represents a verification request to external service
type VerificationRequest struct {
	DocumentType   DocumentType `json:"document_type"`
	DocumentNumber string       `json:"document_number"`
	IssueDate      time.Time    `json:"issue_date"`
	ExpiryDate     time.Time    `json:"expiry_date"`
	FileURL        string       `json:"file_url"`
}

// VerificationResponse represents a verification response
type VerificationResponse struct {
	IsValid           bool    `json:"is_valid"`
	VerificationScore float64 `json:"verification_score"`
	IssuingAuthority  string  `json:"issuing_authority"`
	RejectionReason   string  `json:"rejection_reason,omitempty"`
}

// NewVerifyDocumentHandler creates a new handler
func NewVerifyDocumentHandler(
	db *database.Postgres,
	eventBus eventbus.EventBus,
	verificationService DocumentVerificationService,
) *VerifyDocumentHandler {
	return &VerifyDocumentHandler{
		db:                  db,
		eventBus:          eventBus,
		verificationService: verificationService,
	}
}

// Execute verifies a driver document
func (h *VerifyDocumentHandler) Execute(ctx context.Context, cmd *VerifyDocumentCommand) (*VerificationResult, error) {
	// 1. Validate document type
	if err := h.validateDocumentType(cmd.DocumentType); err != nil {
		return nil, err
	}

	// 2. Load document from database
	document, err := h.loadDocument(ctx, cmd.DocumentID)
	if err != nil {
		return nil, fmt.Errorf("failed to load document: %w", err)
	}

	// 3. Check if already verified
	if document.Status == string(DocumentStatusVerified) {
		return nil, ErrDocumentAlreadyVerified
	}

	// 4. Check expiry
	if !cmd.ExpiryDate.IsZero() && cmd.ExpiryDate.Before(time.Now()) {
		// Mark as expired
		if err := h.updateDocumentStatus(ctx, cmd.DocumentID, DocumentStatusExpired, "Document has expired"); err != nil {
			return nil, err
		}
		return nil, ErrDocumentExpired
	}

	// 5. Call external verification service
	verificationReq := &VerificationRequest{
		DocumentType:   cmd.DocumentType,
		DocumentNumber: cmd.DocumentNumber,
		IssueDate:      cmd.IssueDate,
		ExpiryDate:     cmd.ExpiryDate,
		FileURL:        cmd.FileURL,
	}

	verificationResp, err := h.verificationService.VerifyDocument(ctx, verificationReq)
	if err != nil {
		// Mark as rejected due to verification failure
		_ = h.updateDocumentStatus(ctx, cmd.DocumentID, DocumentStatusRejected, err.Error())
		return nil, fmt.Errorf("%w: %v", ErrVerificationFailed, err)
	}

	// 6. Determine status based on verification result
	var status DocumentStatus
	var rejectionReason string
	if verificationResp.IsValid && verificationResp.VerificationScore >= 0.7 {
		status = DocumentStatusVerified
	} else {
		status = DocumentStatusRejected
		rejectionReason = verificationResp.RejectionReason
		if rejectionReason == "" {
			rejectionReason = fmt.Sprintf("Verification score too low: %.2f", verificationResp.VerificationScore)
		}
	}

	// 7. Update document in database
	if err := h.updateDocumentVerification(ctx, cmd.DocumentID, status, rejectionReason, cmd.ExpiryDate); err != nil {
		return nil, err
	}

	// 8. Update driver verification status if all documents are verified
	if status == DocumentStatusVerified {
		if err := h.updateDriverVerificationStatus(ctx, cmd.DriverID); err != nil {
			// Log but don't fail
		}
	}

	// 9. Emit verification event
	h.emitVerificationEvent(ctx, cmd, status, verificationResp.VerificationScore)

	// 10. Return result
	result := &VerificationResult{
		DocumentID:        cmd.DocumentID,
		Status:            status,
		VerificationScore: verificationResp.VerificationScore,
		RejectionReason:   rejectionReason,
	}
	if status == DocumentStatusVerified {
		now := time.Now().UTC()
		result.VerifiedAt = &now
		if !cmd.ExpiryDate.IsZero() {
			result.ExpiresAt = &cmd.ExpiryDate
		}
	}

	return result, nil
}

// ============================================================================
// VALIDATION
// ============================================================================

func (h *VerifyDocumentHandler) validateDocumentType(docType DocumentType) error {
	validTypes := map[DocumentType]bool{
		DocumentTypeDriversLicense:      true,
		DocumentTypeVehicleRegistration: true,
		DocumentTypeInsurance:           true,
		DocumentTypeBackgroundCheck:     true,
		DocumentTypeVehicleInspection:   true,
	}

	if !validTypes[docType] {
		return ErrInvalidDocumentType
	}
	return nil
}

// ============================================================================
// DATABASE OPERATIONS
// ============================================================================

type driverDocument struct {
	ID         uuid.UUID
	DriverID   uuid.UUID
	DocType    string
	Status     string
	DocumentNumber string
	IssueDate  *time.Time
	ExpiryDate *time.Time
	FileURL    string
}

func (h *VerifyDocumentHandler) loadDocument(ctx context.Context, id uuid.UUID) (*driverDocument, error) {
	var doc driverDocument
	query := `
		SELECT id, driver_id, type, status, document_number, issue_date, expiry_date, file_url
		FROM driver_documents
		WHERE id = $1
	`
	err := h.db.QueryRow(ctx, query, id).Scan(
		&doc.ID, &doc.DriverID, &doc.DocType, &doc.Status,
		&doc.DocumentNumber, &doc.IssueDate, &doc.ExpiryDate, &doc.FileURL,
	)
	if err != nil {
		return nil, ErrDocumentNotFound
	}
	return &doc, nil
}

func (h *VerifyDocumentHandler) updateDocumentStatus(ctx context.Context, id uuid.UUID, status DocumentStatus, reason string) error {
	query := `
		UPDATE driver_documents
		SET status = $1, verification_notes = $2, updated_at = NOW()
		WHERE id = $3
	`
	_, err := h.db.Exec(ctx, query, string(status), reason, id)
	return err
}

func (h *VerifyDocumentHandler) updateDocumentVerification(
	ctx context.Context,
	id uuid.UUID,
	status DocumentStatus,
	reason string,
	expiryDate time.Time,
) error {
	query := `
		UPDATE driver_documents
		SET status = $1, verification_notes = $2, expiry_date = $3, updated_at = NOW()
		WHERE id = $4
	`
	var expiry *time.Time
	if !expiryDate.IsZero() {
		expiry = &expiryDate
	}
	_, err := h.db.Exec(ctx, query, string(status), reason, expiry, id)
	return err
}

func (h *VerifyDocumentHandler) updateDriverVerificationStatus(ctx context.Context, driverID uuid.UUID) error {
	// Check if all required documents are verified
	query := `
		SELECT COUNT(*) = 5 FROM driver_documents
		WHERE driver_id = $1 AND status = 'verified'
		AND type IN ('drivers_license', 'vehicle_registration', 'insurance', 'background_check', 'vehicle_inspection')
	`
	var allVerified bool
	if err := h.db.QueryRow(ctx, query, driverID).Scan(&allVerified); err != nil {
		return err
	}

	if allVerified {
		query = `UPDATE drivers SET license_verified = true, background_check = true, vehicle_inspected = true, updated_at = NOW() WHERE id = $1`
		_, err := h.db.Exec(ctx, query, driverID)
		return err
	}
	return nil
}

// ============================================================================
// EVENT EMISSION
// ============================================================================

func (h *VerifyDocumentHandler) emitVerificationEvent(
	ctx context.Context,
	cmd *VerifyDocumentCommand,
	status DocumentStatus,
	score float64,
) {
	event := eventbus.Event{
		Type: "driver.document.verified",
		Payload: map[string]interface{}{
			"document_id":        cmd.DocumentID,
			"driver_id":          cmd.DriverID,
			"document_type":      cmd.DocumentType,
			"status":             status,
			"verification_score": score,
			"verified_by":        cmd.VerifiedBy,
		},
		Timestamp: time.Now().Unix(),
	}

	go func() {
		_ = h.eventBus.Publish(context.Background(), "driver.onboarding", event)
	}()
}