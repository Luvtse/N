package http

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"nidaw-backend/internal/modules/legal/application/services"
	"nidaw-backend/internal/modules/legal/domain/entities"
)

type ConsentHandler struct {
	consentService *services.ConsentService
}

func NewConsentHandler(consentService *services.ConsentService) *ConsentHandler {
	return &ConsentHandler{consentService: consentService}
}

// GetDocument returns a legal document
func (h *ConsentHandler) GetDocument(w http.ResponseWriter, r *http.Request) {
	docType := r.URL.Query().Get("type")
	language := r.URL.Query().Get("lang")
	region := r.URL.Query().Get("region")

	if language == "" {
		language = "en"
	}
	if region == "" {
		region = "GLOBAL"
	}

	doc, err := h.consentService.GetActiveDocument(r.Context(), docType, language, region)
	if err != nil {
		http.Error(w, "Document not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(doc)
}

// GiveConsent records user consent
func (h *ConsentHandler) GiveConsent(w http.ResponseWriter, r *http.Request) {
	// Extract user ID from context (set by auth middleware)
	userID, ok := r.Context().Value("user_id").(uuid.UUID)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req entities.GiveConsentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Extract metadata
	ipAddress := r.RemoteAddr
	userAgent := r.UserAgent()
	deviceInfo := r.Header.Get("X-Device-Info")

	err := h.consentService.GiveConsent(r.Context(), userID, &req, ipAddress, userAgent, deviceInfo)
	if err != nil {
		if err == services.ErrConsentAlreadyGiven {
			http.Error(w, "Consent already given", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "consent_recorded"})
}

// WithdrawConsent allows user to withdraw consent
func (h *ConsentHandler) WithdrawConsent(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value("user_id").(uuid.UUID)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	docType := r.URL.Query().Get("type")
	reason := r.URL.Query().Get("reason")
	ipAddress := r.RemoteAddr
	userAgent := r.UserAgent()

	err := h.consentService.WithdrawConsent(r.Context(), userID, docType, reason, ipAddress, userAgent)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "consent_withdrawn"})
}

// GetUserConsents returns all consent records for user
func (h *ConsentHandler) GetUserConsents(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value("user_id").(uuid.UUID)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	consents, err := h.consentService.GetUserConsents(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(consents)
}

// CheckConsent verifies if user has given consent
func (h *ConsentHandler) CheckConsent(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value("user_id").(uuid.UUID)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	docType := r.URL.Query().Get("type")

	hasConsent, err := h.consentService.CheckConsent(r.Context(), userID, docType)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"has_consent": hasConsent})
}

// GetAuditLog returns consent audit trail (for compliance requests)
func (h *ConsentHandler) GetAuditLog(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value("user_id").(uuid.UUID)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	logs, err := h.consentService.GetConsentAuditLog(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(logs)
}
