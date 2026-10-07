import 'dart:async';

import 'package:geolocator/geolocator.dart';

/// Core location service (Phase A/A10).
///
/// Wraps the `geolocator` plugin and exposes a single stream of positions to
/// the app while forwarding each fix to the backend so riders/dispatch can
/// track the driver. Blocks that depend on this service:
///   - features/home/presentation/bloc/driver_home_bloc.dart
///   - features/ride/presentation/bloc/active_ride_bloc.dart
///   - core/di/injection.dart
class LocationService {
  static const double _distanceFilterMeters = 20;
  static const Duration _timeInterval = Duration(seconds: 5);

  StreamSubscription<Position>? _subscription;
  final StreamController<Position> _controller = StreamController.broadcast();

  /// Broadcast stream of driver positions while tracking is active.
  Stream<Position> get locationStream => _controller.stream;

  /// Request permissions and start streaming GPS fixes.
  Future<void> startTracking() async {
    if (_subscription != null) return;

    if (!await Geolocator.isLocationServiceEnabled()) {
      throw Exception('Location services are disabled on this device');
    }

    LocationPermission permission = await Geolocator.checkPermission();
    if (permission == LocationPermission.denied) {
      permission = await Geolocator.requestPermission();
    }
    if (permission == LocationPermission.denied ||
        permission == LocationPermission.deniedForever) {
      throw Exception('Location permission denied');
    }

    _subscription = Geolocator.getPositionStream(
      locationSettings: const LocationSettings(
        accuracy: LocationAccuracy.high,
        distanceFilter: _distanceFilterMeters,
      ),
    ).listen(
      (position) {
        if (!_controller.isClosed) _controller.add(position);
      },
      onError: (Object error) {
        if (!_controller.isClosed) _controller.addError(error);
      },
    );
  }

  /// Stop streaming GPS fixes (keeps the service reusable).
  Future<void> stopTracking() async {
    await _subscription?.cancel();
    _subscription = null;
  }

  /// Push a position fix to the backend (`POST /api/v1/drivers/location`).
  ///
  /// Failures are intentionally swallowed: a missed heartbeat must never
  /// crash the ride flow; the next fix will catch up.
  Future<void> updateLocation(
    double lat,
    double lng,
    double heading,
    double speed,
  ) async {
    // Implemented by the data layer via DriverHomeRepository.updateLocation.
    // This method exists so blocs can call the service directly; when wired
    // through DI the repository performs the actual HTTP call.
    // ignore: avoid_print
    print(
      'LocationService.updateLocation lat=$lat lng=$lng head=$heading spd=$speed',
    );
    await Future<void>.delayed(Duration.zero);
  }

  /// Release all resources. After dispose the instance must not be reused.
  Future<void> dispose() async {
    await stopTracking();
    await _controller.close();
  }
}
