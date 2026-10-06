package http

import (
	"nidaw-backend/internal/shared/auth"

	"github.com/go-chi/chi/v5"
)

// RegisterConsentRoutes mounts the legal/consent surface on the chi router.
//
// SECURITY (Phase B / B2, B3):
//   - Consent routes now run behind auth.Service.Middleware() so identity is
//     real and handlers can use typed context extraction.
//   - Admin document mutation endpoints require the "admin" role via
//     auth.Service.RequireRole — previously they had no authentication at all.
func RegisterConsentRoutes(r chi.Router, handler *ConsentHandler, admin *AdminHandler, authService *auth.Service) {
	// Public route: active legal documents are readable without a session.
	r.Get("/api/v1/legal/documents", handler.GetDocument)

	// Authenticated consent operations.
	r.Group(func(pr chi.Router) {
		pr.Use(authService.Middleware())
		pr.Post("/api/v1/consent", handler.GiveConsent)
		pr.Get("/api/v1/consent", handler.GetUserConsents)
		pr.Get("/api/v1/consent/check", handler.CheckConsent)
		pr.Post("/api/v1/consent/withdraw", handler.WithdrawConsent)
		pr.Get("/api/v1/consent/audit", handler.GetAuditLog)
	})

	// Admin-only legal document management + audit export (B2).
	r.Group(func(ar chi.Router) {
		ar.Use(authService.Middleware())
		ar.Use(authService.RequireRole("admin"))
		ar.Post("/api/v1/legal/admin/documents", admin.CreateDocument)
		ar.Delete("/api/v1/legal/admin/documents/{id}", admin.DeactivateDocument)
		ar.Get("/api/v1/legal/admin/stats", admin.GetConsentStats)
		ar.Get("/api/v1/legal/admin/audit/export", admin.ExportConsentAudit)
	})
}
