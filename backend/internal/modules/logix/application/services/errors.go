package services

import "errors"

// ErrNoCarriersAvailable is returned when load matching finds no candidate
// carriers within the required radius/vehicle constraints.
var ErrNoCarriersAvailable = errors.New("no carriers available for this load")
