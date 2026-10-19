// Package services_test holds integration tests for the §3.5 multi-leg
// pricing path (PricingService.CalculateFareWithStops) that both the
// stopover request flow and the mid-trip destination-change command use to
// recompute fares server-side. These assertions back the endpoint contract:
// the fare a rider sees on the change-destination confirmation sheet is the
// fare they pay — computed here, never estimated on the client.
package services_test

import (
	"context"
	"testing"

	nidusservices "nidaw-backend/internal/modules/nidus/application/services"
)

// TestCalculateFareWithStops_ZeroStopEquivalence pins the regression
// guarantee documented on CalculateFareWithStops: with an empty waypoint
// list the multi-leg engine must produce byte-identical money fields to the
// historical single-leg CalculateFare. If this ever drifts, every existing
// ride estimate moves when the stopovers flag lands — unacceptable.
func TestCalculateFareWithStops_ZeroStopEquivalence(t *testing.T) {
	svc := nidusservices.NewPricingService(nil, nidusservices.DefaultPricingConfig())
	ctx := context.Background()

	// Addis Ababa corridor: Bole Airport -> Piassa. Deliberately NOT the NYC
	// demo literals banned by Global Rule 2 / step 1.2.
	const (
		pLat, pLng = 8.9730, 38.7960 // Bole Airport
		dLat, dLng = 9.0340, 38.7500 // Piassa
	)

	single, err := svc.CalculateFare(ctx, pLat, pLng, dLat, dLng, nidusservices.RideTypeStandard)
	if err != nil {
		t.Fatalf("single-leg CalculateFare: %v", err)
	}
	multi, err := svc.CalculateFareWithStops(ctx, pLat, pLng, []nidusservices.Waypoint{{Lat: dLat, Lng: dLng}}, nidusservices.RideTypeStandard)
	if err != nil {
		t.Fatalf("zero-stop CalculateFareWithStops: %v", err)
	}

	// Deterministic fields must match exactly. Surge/pickup estimates read
	// live demand state; with a nil pool both degrade identically (surge 1.0
	// default), so total money equality still holds.
	if multi.BaseFare != single.BaseFare {
		t.Errorf("base fare drifted: multi=%d single=%d", multi.BaseFare, single.BaseFare)
	}
	if multi.DistanceFare != single.DistanceFare {
		t.Errorf("distance fare drifted: multi=%d single=%d", multi.DistanceFare, single.DistanceFare)
	}
	if multi.TimeFare != single.TimeFare {
		t.Errorf("time fare drifted: multi=%d single=%d", multi.TimeFare, single.TimeFare)
	}
	if multi.Subtotal != single.Subtotal {
		t.Errorf("subtotal drifted: multi=%d single=%d", multi.Subtotal, single.Subtotal)
	}
	if multi.TotalFare != single.TotalFare {
		t.Errorf("total fare drifted: multi=%d single=%d", multi.TotalFare, single.TotalFare)
	}
	if multi.DistanceKm != single.DistanceKm {
		t.Errorf("distance km drifted: multi=%.2f single=%.2f", multi.DistanceKm, single.DistanceKm)
	}
	if multi.Currency != single.Currency {
		t.Errorf("currency drifted: multi=%q single=%q", multi.Currency, single.Currency)
	}
}

// TestCalculateFareWithStops_BaseFareChargedOnce pins the core stopover
// semantics from the spec: a stop adds distance/time, never a second
// flag-drop. The two-leg chain A->B->C must carry the SAME BaseFare as the
// direct A->C leg set, while total distance strictly increases.
func TestCalculateFareWithStops_BaseFareChargedOnce(t *testing.T) {
	svc := nidusservices.NewPricingService(nil, nidusservices.DefaultPricingConfig())
	ctx := context.Background()

	const (
		aLat, aLng = 9.0300, 38.7400 // Piassa
		bLat, bLng = 9.0650, 38.8300 // off-corridor detour (north-east)
		cLat, cLng = 8.9800, 38.7900 // near Bole
	)

	direct, err := svc.CalculateFareWithStops(ctx, aLat, aLng, []nidusservices.Waypoint{{Lat: cLat, Lng: cLng}}, nidusservices.RideTypeStandard)
	if err != nil {
		t.Fatalf("direct chain: %v", err)
	}
	viaStop, err := svc.CalculateFareWithStops(ctx, aLat, aLng, []nidusservices.Waypoint{{Lat: bLat, Lng: bLng}, {Lat: cLat, Lng: cLng}}, nidusservices.RideTypeStandard)
	if err != nil {
		t.Fatalf("via-stop chain: %v", err)
	}

	if viaStop.BaseFare != direct.BaseFare {
		t.Errorf("base fare charged per leg: via=%d direct=%d (must be equal — flag drop once)", viaStop.BaseFare, direct.BaseFare)
	}
	if viaStop.DistanceKm <= direct.DistanceKm {
		t.Errorf("stop did not extend distance: via=%.2f direct=%.2f", viaStop.DistanceKm, direct.DistanceKm)
	}
	if viaStop.TotalFare <= direct.TotalFare {
		t.Errorf("stop did not raise total: via=%d direct=%d", viaStop.TotalFare, direct.TotalFare)
	}
	// Subtotal decomposition must stay honest: base + distance + time only.
	if got := viaStop.BaseFare + viaStop.DistanceFare + viaStop.TimeFare; got != viaStop.Subtotal {
		t.Errorf("subtotal no longer decomposes into itemized lines: %d != %d+%d+%d", viaStop.Subtotal, got, viaStop.BaseFare, viaStop.DistanceFare)
	}
}

// TestCalculateFareWithStops_RejectsDegenerateChains pins validation: an
// empty chain, out-of-range coordinates, and zero-length legs are all
// rejected rather than silently priced — the request handler relies on these
// sentinels to return 400 INVALID_STOP_COORDINATES / INVALID_LOCATION.
func TestCalculateFareWithStops_RejectsDegenerateChains(t *testing.T) {
	svc := nidusservices.NewPricingService(nil, nidusservices.DefaultPricingConfig())
	ctx := context.Background()

	if _, err := svc.CalculateFareWithStops(ctx, 9.0, 38.7, nil, nidusservices.RideTypeStandard); err == nil {
		t.Error("empty chain accepted; must require at least a dropoff")
	}

	// Out-of-range latitude on the FINAL waypoint (a dragged bad pin).
	if _, err := svc.CalculateFareWithStops(ctx, 9.0, 38.7, []nidusservices.Waypoint{{Lat: 95.0, Lng: 38.7}}, nidusservices.RideTypeStandard); err == nil {
		t.Error("out-of-range stop accepted; validateChain must reject it")
	}

	// Coincident pickup and dropoff => zero-length leg.
	if _, err := svc.CalculateFareWithStops(ctx, 9.0, 38.7, []nidusservices.Waypoint{{Lat: 9.0, Lng: 38.7}}, nidusservices.RideTypeStandard); err == nil {
		t.Error("zero-length leg accepted; must be rejected")
	}
}
