// Shared helpers for mapping between the payment domain (float64 ETB) and the
// payments.PaymentGateway interface contract (int64 minor units / cents).
package commands

import (
	"math"

	"nidaw-backend/internal/modules/payment/domain/entities"
)

// toCents converts a float64 amount in major currency units to int64 minor
// units, rounding half-up to avoid floating point drift on money values.
func toCents(amount float64) int64 {
	return int64(math.Round(amount * 100))
}

// fromCents converts int64 minor units back to float64 major units.
func fromCents(cents int64) float64 {
	return float64(cents) / 100.0
}

// mapGatewayStatus maps a canonical gateway status string onto the domain
// TransactionStatus enum via an explicit whitelist. Unknown statuses are
// treated as "processing" so reconciliation can resolve them later rather
// than persisting an invalid enum value (audit finding §3.4).
func mapGatewayStatus(gwStatus string) entities.TransactionStatus {
	switch gwStatus {
	case "succeeded":
		return entities.TransactionStatusSucceeded
	case "failed", "canceled", "cancelled":
		return entities.TransactionStatusFailed
	case "requires_payment_method", "requires_confirmation", "pending", "processing":
		return entities.TransactionStatusProcessing
	default:
		// Unmapped provider status: keep as processing; reconciliation job
		// will resolve it against the provider's authoritative state.
		return entities.TransactionStatusProcessing
	}
}
