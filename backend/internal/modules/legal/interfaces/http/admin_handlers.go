package http

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"nidaw-backend/internal/modules/legal/domain/entities"
	"nidaw-backend/internal/shared/database"
)

type AdminHandler struct {
	db *database.Postgres
}

func NewAdminHandler(db *database.Postgres) *AdminHandler {
	return &AdminHandler{db: db}
}

// CreateDocument creates a new legal document version
func (h *AdminHandler) CreateDocument(w http.ResponseWriter, r *http.Request) {
	var doc entities.LegalDocument
	if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	doc.ID = uuid.New()
	doc.CreatedAt = time.Now()
	doc.UpdatedAt = time.Now()

	_, err := h.db.Exec(r.Context(), `
		INSERT INTO legal_documents (id, document_type, version, title, content, content_html, 
		                             language, region, effective_date, is_active, requires_reconsent)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`, doc.ID, doc.DocumentType, doc.Version, doc.Title, doc.Content, doc.ContentHTML,
		doc.Language, doc.Region, doc.EffectiveDate, doc.IsActive, doc.RequiresReconsent)

	if err != nil {
		writeInternalError(w)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(doc)
}

// DeactivateDocument deactivates a document version
func (h *AdminHandler) DeactivateDocument(w http.ResponseWriter, r *http.Request) {
	docID := r.URL.Query().Get("id")

	_, err := h.db.Exec(r.Context(), `
		UPDATE legal_documents
		SET is_active = false, updated_at = NOW()
		WHERE id = $1
	`, docID)

	if err != nil {
		writeInternalError(w)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "deactivated"})
}

// GetConsentStats returns consent statistics
func (h *AdminHandler) GetConsentStats(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(r.Context(), `
		SELECT document_type, 
		       COUNT(*) FILTER (WHERE consent_given = true) as consented,
		       COUNT(*) FILTER (WHERE consent_given = false) as withdrawn,
		       COUNT(*) as total
		FROM user_consents
		GROUP BY document_type
	`)

	if err != nil {
		writeInternalError(w)
		return
	}
	defer rows.Close()

	var stats []map[string]interface{}
	for rows.Next() {
		var docType string
		var consented, withdrawn, total int

		err := rows.Scan(&docType, &consented, &withdrawn, &total)
		if err != nil {
			continue
		}

		stats = append(stats, map[string]interface{}{
			"document_type": docType,
			"consented":     consented,
			"withdrawn":     withdrawn,
			"total":         total,
			"consent_rate":  float64(consented) / float64(total) * 100,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// ExportConsentAudit exports audit log for compliance
func (h *AdminHandler) ExportConsentAudit(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")

	rows, err := h.db.Query(r.Context(), `
		SELECT id, user_id, document_type, action, old_version, new_version,
		       ip_address, user_agent, metadata, created_at
		FROM consent_audit_log
		WHERE user_id = $1
		ORDER BY created_at DESC
	`, userID)

	if err != nil {
		writeInternalError(w)
		return
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
			continue
		}
		logs = append(logs, log)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(logs)
}
