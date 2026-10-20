package services

import (
	"math"
	"testing"

	"github.com/google/uuid"
	"nidaw-backend/internal/modules/nidus/domain/entities"
)

// newTestEngine returns a MatchingEngine with nil db/eventBus. Safe because
// calculateDriverScore, normalizeScoringInputs, clampFloat and calculateETA
// are pure functions that never touch the database or event bus.
func newTestEngine() *MatchingEngine {
	m := NewMatchingEngine(nil, nil)
	return m
}

func scoringTestRide() *entities.Ride {
	return &entities.Ride{
		ID:        uuid.New(),
		PickupLat: 59.9139,
		PickupLng: 10.7522,
	}
}

func TestClampFloat(t *testing.T) {
	cases := []struct {
		name      string
		v, lo, hi float64
		want      float64
	}{
		{"in range", 42, 0, 100, 42},
		{"below lo", -5, 0, 100, 0},
		{"above hi", 105, 0, 100, 100},
		{"nan maps to lo", math.NaN(), 0, 100, 0},
		{"+inf maps to hi", math.Inf(1), 0, 100, 100},
		{"-inf maps to lo", math.Inf(-1), 0, 100, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := clampFloat(tc.v, tc.lo, tc.hi)
			if got != tc.want {
				t.Fatalf("clampFloat(%v,%v,%v) = %v, want %v", tc.v, tc.lo, tc.hi, got, tc.want)
			}
		})
	}
}

func TestNormalizeScoringInputs(t *testing.T) {
	d := entities.Driver{
		Rating:         7.5, // out of 0..5 -> clamp to 5
		AcceptanceRate: 140, // out of 0..100 -> clamp to 100
		CompletionRate: -3,  // out of range -> clamp to 0
	}
	rating, acceptance, completion := normalizeScoringInputs(d)
	if rating != 5 || acceptance != 100 || completion != 0 {
		t.Fatalf("normalizeScoringInputs = (%v,%v,%v), want (5,100,0)", rating, acceptance, completion)
	}

	nan := entities.Driver{Rating: math.NaN(), AcceptanceRate: math.NaN(), CompletionRate: math.NaN()}
	rating, acceptance, completion = normalizeScoringInputs(nan)
	if rating != 0 || acceptance != 0 || completion != 0 {
		t.Fatalf("NaN inputs should map to lower bounds, got (%v,%v,%v)", rating, acceptance, completion)
	}
}

// TestCalculateDriverScoreDeterministic verifies ranking is deterministic and
// bounded: two candidates differing only in one factor must flip rank
// predictably, and corrupt (out-of-range/NaN) inputs must not let a bad driver
// outrank a clean one.
func TestCalculateDriverScoreDeterministic(t *testing.T) {
	m := newTestEngine()
	ride := scoringTestRide()

	// Same location; A has strictly better rating, acceptance, completion.
	locLat, locLng := 59.9150, 10.7540
	a := entities.Driver{ID: uuid.New(), CurrentLat: locLat, CurrentLng: locLng,
		Rating: 4.9, AcceptanceRate: 98, CompletionRate: 99}
	b := entities.Driver{ID: uuid.New(), CurrentLat: locLat, CurrentLng: locLng,
		Rating: 3.0, AcceptanceRate: 60, CompletionRate: 70}

	sa := m.calculateDriverScore(a, ride)
	sb := m.calculateDriverScore(b, ride)
	if !(sa.Score > sb.Score) {
		t.Fatalf("better-quality driver at same location must score higher: A=%v B=%v", sa.Score, sb.Score)
	}

	// Repeat calls produce identical scores (pure function, no randomness).
	sa2 := m.calculateDriverScore(a, ride)
	if sa2.Score != sa.Score || sa2.Distance != sa.Distance || sa2.ETA != sa.ETA {
		t.Fatalf("score not deterministic across calls: %v vs %v", sa, sa2)
	}

	// Corrupt driver (rating 999, NaN acceptance) at same location must be
	// clamped to at most the max achievable quality score, i.e. it must NOT
	// beat the clean high-quality driver A.
	corrupt := entities.Driver{ID: uuid.New(), CurrentLat: locLat, CurrentLng: locLng,
		Rating: 999, AcceptanceRate: math.NaN(), CompletionRate: 1e9}
	sc := m.calculateDriverScore(corrupt, ride)
	if sc.Score > sa.Score+1e-9 {
		t.Fatalf("clamped corrupt driver must not outrank clean best driver: corrupt=%v clean=%v", sc.Score, sa.Score)
	}
	// Clamped values recorded on the result, not raw garbage.
	if sc.Rating != 5 || sc.AcceptanceRate != 0 || sc.CompletionRate != 100 {
		t.Fatalf("DriverScore should carry normalized inputs, got (%v,%v,%v)", sc.Rating, sc.AcceptanceRate, sc.CompletionRate)
	}

	// Score bounded in [0,100] for all cases.
	for name, s := range map[string]float64{"a": sa.Score, "b": sb.Score, "corrupt": sc.Score} {
		if s < 0 || s > 100 {
			t.Fatalf("score %s=%v out of [0,100]", name, s)
		}
	}
}

// TestCalculateDriverScoreDistanceFlipsRank isolates the distance factor:
// identical quality, different proximity -> closer wins.
func TestCalculateDriverScoreDistanceFlipsRank(t *testing.T) {
	m := newTestEngine()
	ride := scoringTestRide()

	near := entities.Driver{ID: uuid.New(), CurrentLat: 59.9145, CurrentLng: 10.7530,
		Rating: 4.5, AcceptanceRate: 90, CompletionRate: 95}
	far := entities.Driver{ID: uuid.New(), CurrentLat: 59.9590, CurrentLng: 10.8750, // ~7 km away
		Rating: 4.5, AcceptanceRate: 90, CompletionRate: 95}

	sn := m.calculateDriverScore(near, ride)
	sf := m.calculateDriverScore(far, ride)
	if !(sn.Score > sf.Score) {
		t.Fatalf("closer driver must win when quality equal: near=%v far=%v", sn.Score, sf.Score)
	}
	if sn.ETA >= sf.ETA {
		t.Fatalf("ETA must reflect actual distance: near ETA=%v far ETA=%v", sn.ETA, sf.ETA)
	}
	if sn.ETA < 1 || sf.ETA < 1 {
		t.Fatalf("heuristic ETA must be >= 1 minute, got %v/%v", sn.ETA, sf.ETA)
	}
}

// TestCalculateDriverScoreNaNCoordinates guards against non-finite locations
// producing NaN scores that would make sort ordering undefined.
func TestCalculateDriverScoreNaNCoordinates(t *testing.T) {
	m := newTestEngine()
	ride := scoringTestRide()
	d := entities.Driver{ID: uuid.New(), CurrentLat: math.NaN(), CurrentLng: 10.0,
		Rating: 4.0, AcceptanceRate: 80, CompletionRate: 80}
	s := m.calculateDriverScore(d, ride)
	if math.IsNaN(s.Score) || math.IsInf(s.Score, 0) {
		t.Fatalf("score must remain finite for NaN coordinates, got %v", s.Score)
	}
	if s.ETA < 1 {
		t.Fatalf("ETA must stay >= 1, got %v", s.ETA)
	}
}
