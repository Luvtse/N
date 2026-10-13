package infrastructure

import (
	"context"
	"errors"
	"time"

	selfieapp "nidaw-backend/internal/modules/selfie/application"
	"nidaw-backend/internal/shared/database"

	"github.com/google/uuid"
)

// RideStoreLookup implements application.RideLookup over the rides table.
// Read-only: it never mutates ride state, keeping module boundaries intact.
type RideStoreLookup struct {
	db *database.Postgres
}

// NewRideStoreLookup constructs the lookup over the shared Postgres handle.
func NewRideStoreLookup(db *database.Postgres) *RideStoreLookup {
	return &RideStoreLookup{db: db}
}

// ErrUnresolvable signals a DB outage or unknown ride — reported honestly
// rather than disguised as "no matched driver".
var ErrUnresolvable = errors.New("ride participants could not be resolved")

// RideDriver returns the matched driver id for a ride. Returns
// application.ErrNoMatchedDriver when the ride has no driver yet.
func (l *RideStoreLookup) RideDriver(rideID uuid.UUID) (uuid.UUID, error) {
	if l.db == nil || !l.db.IsReady() {
		return uuid.Nil, ErrUnresolvable
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var driverID *uuid.UUID
	err := l.db.QueryRow(ctx, `SELECT driver_id FROM rides WHERE id = $1`, rideID).Scan(&driverID)
	if err != nil {
		return uuid.Nil, ErrUnresolvable
	}
	if driverID == nil {
		return uuid.Nil, selfieapp.ErrNoMatchedDriver
	}
	return *driverID, nil
}

// RideRider returns the rider user id that owns the ride.
func (l *RideStoreLookup) RideRider(rideID uuid.UUID) (uuid.UUID, error) {
	if l.db == nil || !l.db.IsReady() {
		return uuid.Nil, ErrUnresolvable
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var userID uuid.UUID
	err := l.db.QueryRow(ctx, `SELECT user_id FROM rides WHERE id = $1`, rideID).Scan(&userID)
	if err != nil {
		return uuid.Nil, ErrUnresolvable
	}
	return userID, nil
}

var _ selfieapp.RideLookup = (*RideStoreLookup)(nil)
