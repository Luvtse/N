// Package domain holds the Pre-Selfie Verification aggregate (§2.3 of the
// ride-parity plan). A driver captures a selfie at the start of DriverEnRoute,
// uploads it, and the rider gets a short-lived signed URL (15 minutes) to
// verify the person behind the wheel before boarding.
//
// Design rules honored here:
//   - No fabricated values: an absent or expired selfie is represented by an
//     explicit state string ("not_sent" / "expired"), never a blank field and
//     never a placeholder image URL.
//   - The repository port has a Postgres implementation for production and an
//     in-memory implementation for tests/dev fallback (Global Rule 5 pairing).
package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// SelfieTTL is how long an uploaded pre-selfie stays redeemable by the rider.
// Locked at 15 minutes by the plan (§2.3: "short-lived signed URL (15 min)").
const SelfieTTL = 15 * time.Minute

// MaxImageBytes is the server-side hard cap on upload size. The driver app
// compresses to <=200 KB before uploading; the server enforces the same bound
// so a malicious or broken client cannot store arbitrarily large blobs.
const MaxImageBytes = 200 * 1024

// AllowedContentTypes restricts uploads to real image encodings. Anything
// else (SVG with scripts, polyglot files, ...) is rejected before storage.
var AllowedContentTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
}

// Status describes the lifecycle of a pre-selfie row.
type Status string

const (
	// StatusUploaded means bytes are durably stored and a signed URL can be
	// minted while ExpiresAt is in the future.
	StatusUploaded Status = "uploaded"

	// StatusExpired means the row exists but the rider's view window has
	// closed. The bytes may still exist for audit purposes; they are simply
	// no longer served to riders.
	StatusExpired Status = "expired"
)

// RiderState values surface on the ride DTO so the rider UI can render an
// honest state instead of guessing from a null URL.
const (
	RiderStateAvailable = "available" // driver_pre_selfie_url is fresh
	RiderStateNotSent   = "not_sent"  // driver did not send a selfie
	RiderStateExpired   = "expired"   // selfie was sent but the window closed
)

var (
	ErrNotFound        = errors.New("pre-selfie not found")
	ErrInvalidRideID   = errors.New("ride id is required")
	ErrInvalidDriverID = errors.New("driver id is required")
	ErrEmptyImage      = errors.New("image body is empty")
	ErrImageTooLarge   = errors.New("image exceeds the 200 KB limit")
	ErrBadContentType  = errors.New("content type must be one of image/jpeg, image/png, image/webp")
	ErrForbidden       = errors.New("only the matched driver of this ride can upload a pre-selfie")
	ErrStorageFailed   = errors.New("image storage backend unavailable")
)

// PreSelfie is the aggregate root: one selfie per (ride, driver), replaced on
// re-upload (idempotent PUT semantics keyed by ride+driver).
type PreSelfie struct {
	ID          uuid.UUID `json:"id" db:"id"`
	RideID      uuid.UUID `json:"ride_id" db:"ride_id"`
	DriverID    uuid.UUID `json:"driver_id" db:"driver_id"`
	ImageURL    string    `json:"image_url" db:"image_url"`
	SizeBytes   int64     `json:"size_bytes" db:"size_bytes"`
	ContentType string    `json:"content_type" db:"content_type"`
	Status      Status    `json:"status" db:"status"`
	CapturedAt  time.Time `json:"captured_at" db:"captured_at"`
	ExpiresAt   time.Time `json:"expires_at" db:"expires_at"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// IsRedeemable reports whether the rider can still view this selfie at time
// now. Expiry is evaluated against the supplied clock, never wall-clock
// inside the entity, so tests stay deterministic.
func (p *PreSelfie) IsRedeemable(now time.Time) bool {
	return p.Status == StatusUploaded && now.Before(p.ExpiresAt)
}

// ValidateUpload checks the raw upload inputs before any bytes touch storage.
func ValidateUpload(rideID, driverID uuid.UUID, contentType string, size int64) error {
	if rideID == uuid.Nil {
		return ErrInvalidRideID
	}
	if driverID == uuid.Nil {
		return ErrInvalidDriverID
	}
	if size <= 0 {
		return ErrEmptyImage
	}
	if size > MaxImageBytes {
		return ErrImageTooLarge
	}
	if !AllowedContentTypes[contentType] {
		return ErrBadContentType
	}
	return nil
}

// Repository persists PreSelfie aggregates. Implementations: Postgres
// (production) and in-memory (tests/dev fallback).
type Repository interface {
	// Upsert stores the selfie keyed by (RideID, DriverID), replacing any
	// prior row for the same pair. Returns the stored aggregate with IDs and
	// timestamps populated.
	Upsert(selfie PreSelfie) (PreSelfie, error)
	// FindByRide returns the most recent selfie for a ride, or ErrNotFound.
	FindByRide(rideID uuid.UUID) (*PreSelfie, error)
	// FindByID returns a selfie by its primary key, or ErrNotFound.
	FindByID(id uuid.UUID) (*PreSelfie, error)
}
