package http

import (
	"net/http"

	"github.com/gorilla/mux"
)

func RegisterConsentRoutes(router *mux.Router, handler *ConsentHandler) {
	// Public routes (no auth required)
	router.HandleFunc("/api/v1/legal/documents", handler.GetDocument).Methods("GET")
	
	// Protected routes (auth required)
	protected := router.PathPrefix("/api/v1/consent").Subrouter()
	protected.HandleFunc("", handler.GiveConsent).Methods("POST")
	protected.HandleFunc("", handler.GetUserConsents).Methods("GET")
	protected.HandleFunc("/check", handler.CheckConsent).Methods("GET")
	protected.HandleFunc("/withdraw", handler.WithdrawConsent).Methods("POST")
	protected.HandleFunc("/audit", handler.GetAuditLog).Methods("GET")
}