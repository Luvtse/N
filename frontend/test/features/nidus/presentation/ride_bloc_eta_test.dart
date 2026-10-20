// Phase 3 — Matching quality and ETA correctness: bloc-level tests.
//
// These exercise the two client paths that previously LOST the selected
// driver's authoritative ETA:
//
//  1. WS `ride.matched` payload with top-level `eta_minutes` but NO embedded
//     driver sub-object. Before the fix the bloc dropped the whole matched
//     transition in that shape (it required update.driver != null). Now it
//     must fall back to the last-known driver from the most recent REST
//     LoadRide and emit DriverMatched carrying the live WS ETA.
//  2. REST reload parity: GET /rides/{id} persists the assignment-time ETA in
//     `matched_eta_minutes`; _emitStateForRide must prefer that authoritative
//     value over the driver card, and when attachDriverCard pairs them they
//     agree — so a reconnect converges on the same number the WS delivered.
//
// Fakes are minimal: RideRepository/SafetyRepository members not needed by
// these scenarios throw loudly via noSuchMethod instead of returning fakes.

import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:nidaw_app/features/nidus/data/repositories/ride_repository.dart';
import 'package:nidaw_app/features/nidus/data/repositories/safety_repository.dart';
import 'package:nidaw_app/features/nidus/domain/usecases/get_ride_status_usecase.dart';
import 'package:nidaw_app/features/nidus/domain/usecases/request_ride_usecase.dart';
import 'package:nidaw_app/features/nidus/presentation/bloc/ride_bloc.dart';

/// Minimal fake repository: serves one canned ride via getRide and pushes
/// whatever the test injects onto the WS subscription stream.
class FakeRideRepository implements RideRepository {
  Ride? nextRide;
  final StreamController<RideStatusUpdate> updates =
      StreamController<RideStatusUpdate>.broadcast();

  @override
  Stream<RideStatusUpdate> subscribeToRideUpdates(String rideId) =>
      updates.stream;

  @override
  void unsubscribeFromRideUpdates(String rideId) {}

  @override
  Future<Ride?> getRide(String rideId) async =>
      nextRide?.id == rideId ? nextRide : null;

  @override
  Future<Ride?> getActiveRide() async => nextRide;

  @override
  dynamic noSuchMethod(Invocation invocation) {
    throw UnsupportedError(
        'FakeRideRepository.${invocation.memberName} must not be reached '
        'by the Phase 3 ETA scenarios');
  }
}

class FakeSafetyRepository implements SafetyRepository {
  @override
  dynamic noSuchMethod(Invocation invocation) {
    throw UnsupportedError(
        'SafetyRepository.${invocation.memberName} must not be reached '
        'by the Phase 3 ETA scenarios');
  }
}

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
  if (matchedEtaMinutes != null) {
    json['matched_eta_minutes'] = matchedEtaMinutes;
  }
  return json;
}

Map<String, dynamic> driverCardJson(int etaMinutes) => {
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
  late FakeRideRepository repo;
  late RideBloc bloc;

  setUp(() {
    repo = FakeRideRepository();
    bloc = RideBloc(
      rideRepository: repo,
      requestRideUseCase: RequestRideUseCase(repo),
      getRideStatusUseCase: GetRideStatusUseCase(repo),
      safetyRepository: FakeSafetyRepository(),
    );
  });

  tearDown(() async {
    await repo.updates.close();
    bloc.close();
  });

  /// Drives the bloc through: REST load (caches driver + persisted ETA) then
  /// subscribes to WS updates.
  Future<void> loadAndSubscribe() async {
    bloc.add(LoadRide('ride-1'));
    // Let the async LoadRide handler settle before subscribing.
    await Future<void>.delayed(Duration.zero);
    bloc.add(const SubscribeToRideUpdates('ride-1'));
    await Future<void>.delayed(Duration.zero);
  }

  group('WS matched payload without driver object (Phase 3 regression)', () {
    test('transition is NOT dropped; emits DriverMatched with the live WS ETA '
        'and the REST-cached driver', () async {
      // REST DTO as attachDriverCard now ships it: driver card under the
      // singular `driver` key, authoritative persisted ETA alongside.
      repo.nextRide = Ride.fromJson(minimalRideJson(matchedEtaMinutes: 7)
        ..['driver'] = driverCardJson(7));

      await loadAndSubscribe();
      expect(bloc.state, isA<DriverMatched>());
      final restState = bloc.state as DriverMatched;
      expect(restState.etaMinutes, 7);

      // Now the bare outbox-style WS event: eta_minutes at top level, no
      // `driver` sub-object. Pre-fix this was silently dropped.
      final received = <RideState>[];
      final sub = bloc.stream.listen(received.add);
      repo.updates.add(RideStatusUpdate(
        rideId: 'ride-1',
        status: 'matched',
        etaMinutes: 6,
        timestamp: DateTime.parse('2026-10-20T09:01:30Z'),
      ));
      await Future<void>.delayed(Duration.zero);

      final matched = received.whereType<DriverMatched>().toList();
      expect(matched, isNotEmpty,
          reason: 'bare matched WS payload must still transition');
      // Live WS value wins per the documented precedence chain.
      expect(matched.last.etaMinutes, 6);
      // Driver identity comes from the REST-authoritative cache, not thin air.
      expect(matched.last.driver.id, 'driver-1');
      await sub.cancel();
    });

    test('precedence falls through to persisted matchedEtaMinutes when the WS '
        'payload omits eta_minutes', () async {
      repo.nextRide = Ride.fromJson(minimalRideJson(matchedEtaMinutes: 7)
        ..['driver'] = driverCardJson(7));

      await loadAndSubscribe();

      final received = <RideState>[];
      final sub = bloc.stream.listen(received.add);
      repo.updates.add(RideStatusUpdate(
        rideId: 'ride-1',
        status: 'matched',
        timestamp: DateTime.parse('2026-10-20T09:01:30Z'),
      ));
      await Future<void>.delayed(Duration.zero);

      final matched = received.whereType<DriverMatched>().toList();
      expect(matched, isNotEmpty);
      // update.etaMinutes null → last-known REST authoritative value (7).
      expect(matched.last.etaMinutes, 7);
      await sub.cancel();
    });

    test('no driver ever seen → stay put rather than fabricating a driver '
        'card (never emit DriverMatched with invented identity)', () async {
      // Cold start: never loaded the ride, so _lastKnownDriver is null.
      bloc.add(const SubscribeToRideUpdates('ride-1'));
      await Future<void>.delayed(Duration.zero);

      final received = <RideState>[];
      final sub = bloc.stream.listen(received.add);
      repo.updates.add(RideStatusUpdate(
        rideId: 'ride-1',
        status: 'matched',
        etaMinutes: 6,
        timestamp: DateTime.parse('2026-10-20T09:01:30Z'),
      ));
      await Future<void>.delayed(Duration.zero);

      expect(received.whereType<DriverMatched>(), isEmpty);
      await sub.cancel();
    });
  });

  group('REST reload convergence (Phase 3 gate: rider/driver see the same '
      'authoritative state after reconnect)', () {
    test('cold re-entry into tracking resolves the persisted ETA, matching '
        'what the WS event delivered live', () async {
      const wsEta = 9;
      repo.nextRide = Ride.fromJson(minimalRideJson(matchedEtaMinutes: wsEta)
        ..['driver'] = driverCardJson(wsEta));

      bloc.add(LoadRide('ride-1'));
      await Future<void>.delayed(Duration.zero);

      final matched = bloc.state;
      expect(matched, isA<DriverMatched>());
      expect((matched as DriverMatched).etaMinutes, wsEta);
    });

    test('legacy row without persisted column falls back to a positive driver-'
        'card value (documented heuristic tier)', () async {
      repo.nextRide = Ride.fromJson(minimalRideJson()
        ..['driver'] = driverCardJson(4));

      bloc.add(LoadRide('ride-1'));
      await Future<void>.delayed(Duration.zero);

      final matched = bloc.state as DriverMatched;
      expect(matched.etaMinutes, 4);
    });

    test('all ETA sources empty → 0 sentinel surfaced honestly (nothing '
        'fabricated); driver card itself still real', () async {
      // DriverInfo requires a non-null int; a legacy zero encodes "unknown"
      // and the UI hides the chip. The bloc must pass it through untouched
      // rather than replacing it with a constant like 5.
      repo.nextRide = Ride.fromJson(minimalRideJson()
        ..['driver'] = driverCardJson(0));

      bloc.add(LoadRide('ride-1'));
      await Future<void>.delayed(Duration.zero);

      final matched = bloc.state as DriverMatched;
      expect(matched.etaMinutes, 0,
          reason: 'no source carried a real ETA — never invent one');
    });
  });
}
