package http

import (
	"os"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"nidaw-backend/internal/modules/ledger/application/commands"
	"nidaw-backend/internal/shared/auth"
)

// NewRouter builds the standalone /api/v1/ledger sub-router (Phase D Step 6).
// All routes sit behind JWT validation (auth.Service.Middleware); identity is
// then read from the request context — client-supplied user ids in bodies are
// ignored entirely (Phase B/B1). The handler additionally self-guards its
// admin-only mutations with requireAdmin, so this router is safe to mount as
// a single chi Group.
func NewRouter(deps *commands.Deps, authService *auth.Service, logger *zap.Logger) chi.Router {
	r := chi.NewRouter()

	h := NewHandler(deps, logger, NewReportSignerFromEnv(logger))

	r.Group(func(pr chi.Router) {
		pr.Use(authService.Middleware())
		h.Register(pr)
	})

	return r
}

// NewReportSignerFromEnv constructs the audit-report signer from the
// LEDGER_REPORT_KEY environment variable. When unset, reports are returned
// unsigned and a warning is logged (dev environments only — Phase B/B9 keeps
// secrets out of the repo; production injects them via the secret manager).
func NewReportSignerFromEnv(logger *zap.Logger) ReportSigner {
	if logger == nil {
		logger = zap.NewNop()
	}
	key := os.Getenv("LEDGER_REPORT_KEY")
	if key == "" {
		logger.Warn("ledger: LEDGER_REPORT_KEY unset — audit reports will be UNSIGNED")
		return nil
	}
	s, err := NewHMACReportSigner(key)
	if err != nil {
		logger.Error("ledger: failed to build report signer", zap.Error(err))
		return nil
	}
	return s
}
