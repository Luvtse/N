// Phase 3 — Matching quality and ETA correctness: DTO-layer unit tests.
//
// These tests pin the client-side half of the "the rider sees the selected
// driver's ACTUAL calculated ETA" requirement at the parsing boundary, where
// the authoritative backend value can silently get lost:
//
//  1. `Ride.fromJson` must read `matched_eta_minutes` (the column persisted
//     atomically inside the assignment transaction, migration 014) as an
//     honest nullable — absent stays null, never 0-defaulted, so the bloc's
//     precedence chain (`update.etaMinutes ?? ride.matchedEtaMinutes ??
//     driver.etaMinutes`) cannot mistake a missing value for "zero minutes".
//  2. `RideStatusUpdate.fromJson` must capture top-level `eta_minutes` from
//     WS `ride.matched` payloads, which carry the ETA but NOT a driver
//     sub-object (the ws_bridge copies outbox payload keys verbatim).
//  3. REST-reload parity: after reconnect/resync the REST DTO's driver card
//     ETA and the persisted `matched_eta_minutes` must agree with what the
//     WS event delivered live.
//
// Pure DTO tests only — no repository instance, network, or bloc needed, so
// they run anywhere `flutter test` runs.

import 'package:flutter_test/flutter_test.dart';
import 'package:nidaw_app/features/nidus/data/repositories/ride_repository.dart';

Map<String, dynamic> minimalRideJson({Object? matchedEtaMinutes}) {
  final json = <String, dynamic>{
    'id': 'ride-1',
    'user_id': 'user-1',
    'driver_id': 'driver-1',
    'pickup_lat': 8.98,
    'pickup_lng': 38.75,
    'dropoff_lat': 9.03,
    'dropoff_lng': 38.79,
    'ride_type': 'standard',
    'status': 'matched',
    'currency': 'ETB',
    'requested_at': '2026-10-20T09:00:00Z',
    'matched_at': '2026-10-20T09:01:00Z',
  };
  // Omitting the key entirely models a pre-migration-014 row or an
  // AllowNilETA assignment; explicit null models JSON null from the API.
  if (matchedEtaMinutes != null) {
    json['matched_eta_minutes'] = matchedEtaMinutes;
  }
  return json;
}

Map<String, dynamic> driverCardJson({int etaMinutes = 7}) => {
      'id': 'driver-1',
      'name': 'Abebe K.',
      'rating': 4.8,
      'vehicle_type': 'sedan',
      'vehicle_plate': '3-ABC-123',
      'current_lat': 8.99,
      'current_lng': 38.76,
      'eta_minutes': etaMinutes,
    };

void main() {
  group('Ride.fromJson matched_eta_minutes (Phase 3 ETA correctness)', () {
    test('parses numeric matched_eta_minutes to int', () {
      final ride = Ride.fromJson(minimalRideJson(matchedEtaMinutes: 7));
      expect(ride.matchedEtaMinutes, 7);
    });

    test('parses JSON numbers that arrive as doubles (Go float encoding)', () {
      final ride = Ride.fromJson(minimalRideJson(matchedEtaMinutes: 4.0));
      expect(ride.matchedEtaMinutes, 4);
    });

    test('absent key stays null — never defaulted to 0 or any constant', () {
      final ride = Ride.fromJson(minimalRideJson());
      expect(ride.matchedEtaMinutes, isNull);
    });

    test('explicit JSON null stays null (AllowNilETA assignments)', () {
      final json = minimalRideJson();
      json['matched_eta_minutes'] = null;
      final ride = Ride.fromJson(json);
      expect(ride.matchedEtaMinutes, isNull);
    });

    test('non-numeric garbage is treated as unknown (null), not coerced', () {
      final json = minimalRideJson();
      json['matched_eta_minutes'] = 'soon';
      final ride = Ride.fromJson(json);
      expect(ride.matchedEtaMinutes, isNull);
    });

    test('REST reload carries both the persisted ETA and the driver card',
        () {
      final json = minimalRideJson(matchedEtaMinutes: 7);
      // Ride.fromJson reads the embedded driver card from the singular
      // `driver` key (verified against lib/…/ride_repository.dart:303).
      json['driver'] = driverCardJson(etaMinutes: 7);
      final ride = Ride.fromJson(json);
      // Parity invariant asserted at the DTO layer: the two independent
      // server-sourced ETA fields agree, exactly as attachDriverCard pairs
      // resp.MatchedETAMinutes with the card for matched rides.
      expect(ride.matchedEtaMinutes, 7);
      expect(ride.driver?.etaMinutes, 7);
      expect(ride.matchedEtaMinutes, ride.driver?.etaMinutes);
    });
  });

  group('RideStatusUpdate.fromJson eta_minutes (WS matched path)', () {
    test('bare WS matched payload: top-level eta_minutes is captured even '
        'with no driver sub-object (bloc uses _lastKnownDriver fallback)', () {
      final update = RideStatusUpdate.fromJson({
        'type': 'ride.matched',
        'ride_id': 'ride-1',
        'status': 'matched',
        'driver_id': 'driver-1',
        'eta_minutes': 6,
        'timestamp': '2026-10-20T09:01:00Z',
      });
      expect(update.status, 'matched');
      expect(update.etaMinutes, 6);
      expect(update.driver, isNull);
    });

    test('envelope-wrapped payload unwraps data{} and keeps the ETA', () {
      final update = RideStatusUpdate.fromJson({
        'event': 'ride.matched',
        'data': {
          'ride_id': 'ride-1',
          'status': 'matched',
          'eta_minutes': 6,
        },
      });
      expect(update.rideId, 'ride-1');
      expect(update.etaMinutes, 6);
    });

    test('missing eta_minutes stays null so the precedence chain falls '
        'through to ride.matchedEtaMinutes / driver.etaMinutes', () {
      final update = RideStatusUpdate.fromJson({
        'ride_id': 'ride-1',
        'status': 'matched',
      });
      expect(update.etaMinutes, isNull);
    });

    test('string-encoded eta is NOT coerced (unknown, not fabricated)', () {
      final update = RideStatusUpdate.fromJson({
        'ride_id': 'ride-1',
        'status': 'matched',
        'eta_minutes': 'six',
      });
      expect(update.etaMinutes, isNull);
    });

    test('payload embedding a full driver card parses both ETA surfaces', () {
      final update = RideStatusUpdate.fromJson({
        'ride_id': 'ride-1',
        'status': 'matched',
        'eta_minutes': 5,
        'eta_confidence': 0.82,
        'driver': driverCardJson(etaMinutes: 5),
      });
      expect(update.etaMinutes, 5);
      expect(update.etaConfidence, 0.82);
      expect(update.driver?.etaMinutes, 5);
    });
  });

  group('Backend→client ETA propagation end-to-end (DTO layer)', () {
    test('live WS ETA and later REST reload converge on the same number', () {
      // What the outbox `ride.matched` event carried at commit time…
      final ws = RideStatusUpdate.fromJson({
        'ride_id': 'ride-1',
        'status': 'matched',
        'eta_minutes': 9,
      });
      // …and what GET /rides/ride-1 returns after reconnect (same persisted
      // rides.matched_eta_minutes column feeding both responses).
      final rest = Ride.fromJson(minimalRideJson(matchedEtaMinutes: 9)
        ..['driver'] = driverCardJson(etaMinutes: 9));

      final resolvedLive = ws.etaMinutes ??
          rest.matchedEtaMinutes ??
          (rest.driver != null && rest.driver!.etaMinutes > 0
              ? rest.driver!.etaMinutes
              : null);
      final resolvedReload = (null as int?) ??
          rest.matchedEtaMinutes ??
          (rest.driver != null && rest.driver!.etaMinutes > 0
              ? rest.driver!.etaMinutes
              : null);

      expect(resolvedLive, 9);
      expect(resolvedReload, 9);
      expect(resolvedLive, resolvedReload);
    });

    test('legacy row without persisted ETA still resolves via driver card',
        () {
      final rest = Ride.fromJson(minimalRideJson()
        ..['driver'] = driverCardJson(etaMinutes: 4));
      final resolved = rest.matchedEtaMinutes ??
          (rest.driver != null && rest.driver!.etaMinutes > 0
              ? rest.driver!.etaMinutes
              : null);
      expect(resolved, 4);
    });

    test('all sources empty → null (UI omits the chip; nothing fabricated)',
        () {
      final rest = Ride.fromJson(minimalRideJson());
      final resolved = rest.matchedEtaMinutes ??
          (rest.driver != null && rest.driver!.etaMinutes > 0
              ? rest.driver!.etaMinutes
              : null);
      expect(resolved, isNull);
    });
  });
}
