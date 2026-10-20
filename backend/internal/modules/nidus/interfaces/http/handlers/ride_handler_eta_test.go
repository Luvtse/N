package handlers

import (
	"context"
	"testing"

	"nidaw-backend/internal/modules/nidus/application/services"
	"nidaw-backend/internal/modules/nidus/domain/entities"
)

// fakeMatchingEngine implements the minimal subset of the matching-engine
// dependency attachDriverCard uses: GetDriverByID. All other methods are
// provided by embedding the real interface via a struct with panic stubs —
// but since RideHandler holds a concrete *services.MatchingEngine we instead
// construct one and override its driver lookup through the exported DB seam.
// To keep this test hermetic we exercise the pure tiering helpers directly,
// which is where the Phase 3 defect lived (persisted ETA never reaching the
// driver card). The end-to-end attachDriverCard wiring is covered by the
// integration suite.

func mustInt(i int) *int { return &i }

func TestResolveDriverCardETA_Tier1LiveOverridesPersisted(t *testing.T) {
	persisted := mustInt(7)
	live := &liveETAResult{minutes: 4, confidence: 0.9}

	got := resolveDriverCardETA(entities.RideStatusMatched, persisted, live)
	if got.Minutes != 4 {
		t.Fatalf("live tier-1 ETA should win while driver approaches pickup, got %d", got.Minutes)
	}
	if got.Confidence == nil || *got.Confidence != 0.9 {
		t.Fatalf("confidence must ride along with tier-1 ETA, got %v", got.Confidence)
	}
}

func TestResolveDriverCardETA_LiveIgnoredOutsideApproachStates(t *testing.T) {
	persisted := mustInt(6)
	live := &liveETAResult{minutes: 3, confidence: 0.8}

	for _, status := range []entities.RideStatus{entities.RideStatusInProgress, entities.RideStatusCompleted} {
		got := resolveDriverCardETA(status, persisted, live)
		if got.Minutes != 6 {
			t.Fatalf("status %s: persisted tier-2 ETA must be kept, live ignored, got %d", status, got.Minutes)
		}
		if got.Confidence != nil {
			t.Fatalf("status %s: no confidence without tier-1 result", status)
		}
	}
}

func TestResolveDriverCardETA_FallsBackToPersistedWhenLiveUnavailable(t *testing.T) {
	cases := []struct {
		name string
		live *liveETAResult
	}{
		{"service unwired", nil},
		{"service error", nil},
		{"non-positive minutes", &liveETAResult{minutes: 0, confidence: 0.5}},
	}
	for _, tc := range cases {
		got := resolveDriverCardETA(entities.RideStatusDriverEnRoute, mustInt(9), tc.live)
		if got.Minutes != 9 {
			t.Fatalf("%s: expected persisted fallback 9, got %d", tc.name, got.Minutes)
		}
		if got.Confidence != nil {
			t.Fatalf("%s: fallback carries no traffic confidence", tc.name)
		}
	}
}

func TestResolveDriverCardETA_HonestAbsenceWhenNoSource(t *testing.T) {
	got := resolveDriverCardETA(entities.RideStatusMatched, nil, nil)
	if got.Minutes != 0 || got.Confidence != nil {
		t.Fatalf("tier 3 must be zero-value honest absence, got %+v", got)
	}
	zero := 0
	got = resolveDriverCardETA(entities.RideStatusMatched, &zero, nil)
	if got.Minutes != 0 {
		t.Fatalf("non-positive persisted value must not surface, got %d", got.Minutes)
	}
}

func TestApplyDriverCardETA_WritesFields(t *testing.T) {
	info := &DriverInfo{}
	conf := 0.75
	applyDriverCardETA(info, resolvedETA{Minutes: 5, Confidence: &conf})
	if info.ETAMinutes != 5 || info.EtaConfidence == nil || *info.EtaConfidence != 0.75 {
		t.Fatalf("apply failed: %+v", info)
	}
	info2 := &DriverInfo{}
	applyDriverCardETA(info2, resolvedETA{Minutes: 0})
	if info2.ETAMinutes != 0 || info2.EtaConfidence != nil {
		t.Fatalf("honest absence must leave fields untouched: %+v", info2)
	}
}

// Guard: liveResultFromPrediction maps the ETAService prediction into the
// internal tier-1 shape; nil in => nil out so the resolver falls through.
func TestLiveResultFromPrediction_NilSafe(t *testing.T) {
	if liveResultFromPrediction(nil) != nil {
		t.Fatal("nil prediction must map to nil")
	}
	p := &services.ETAPrediction{DurationMinutes: 12, Confidence: 0.42}
	r := liveResultFromPrediction(p)
	if r == nil || r.minutes != 12 || r.confidence != 0.42 {
		t.Fatalf("prediction mapping wrong: %+v", r)
	}
}

var _ = context.Background
