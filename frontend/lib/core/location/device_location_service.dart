import 'dart:async';

import 'package:geolocator/geolocator.dart';
import 'package:latlong2/latlong.dart';

/// Outcome of a location request, as a discriminated result so callers must
/// handle every failure mode explicitly (no silent nulls).
sealed class LocationResult {
  const LocationResult();

  bool get isGranted => this is LocationGranted;
}

class LocationGranted extends LocationResult {
  final double latitude;
  final double longitude;

  /// Accuracy radius in meters reported by the platform.
  final double accuracyMeters;

  /// True when this came from `getLastKnownPosition` rather than a fresh fix.
  final bool isLastKnown;

  const LocationGranted({
    required this.latitude,
    required this.longitude,
    required this.accuracyMeters,
    this.isLastKnown = false,
  });

  LatLng toLatLng() => LatLng(latitude, longitude);
}

class LocationPermissionDenied extends LocationResult {
  const LocationPermissionDenied();
}

class LocationPermissionPermanentlyDenied extends LocationResult {
  const LocationPermissionPermanentlyDenied();
}

class LocationServiceDisabled extends LocationResult {
  const LocationServiceDisabled();
}

class LocationTimeout extends LocationResult {
  const LocationTimeout();
}

enum LocationAccuracyLevel { low, balanced, high }

/// Wraps geolocator with the permission/service/timeout states the UI needs.
/// Single source of truth for GPS on both Request and Tracking surfaces.
class DeviceLocationService {
  const DeviceLocationService();

  /// One-shot position read. Never throws: every failure is a typed result.
  Future<LocationResult> getCurrentPosition({
    LocationAccuracyLevel accuracy = LocationAccuracyLevel.high,
    Duration timeout = const Duration(seconds: 15),
  }) async {
    final serviceEnabled = await Geolocator.isLocationServiceEnabled();
    if (!serviceEnabled) return const LocationServiceDisabled();

    var permission = await Geolocator.checkPermission();
    if (permission == LocationPermission.denied) {
      permission = await Geolocator.requestPermission();
    }
    if (permission == LocationPermission.denied) {
      return const LocationPermissionDenied();
    }
    if (permission == LocationPermission.deniedForever) {
      return const LocationPermissionPermanentlyDenied();
    }

    try {
      final position = await Geolocator.getCurrentPosition(
        locationSettings: LocationSettings(
          accuracy: switch (accuracy) {
            LocationAccuracyLevel.low => LocationAccuracy.low,
            LocationAccuracyLevel.balanced => LocationAccuracy.medium,
            LocationAccuracyLevel.high => LocationAccuracy.high,
          },
        ),
      ).timeout(timeout);
      return LocationGranted(
        latitude: position.latitude,
        longitude: position.longitude,
        accuracyMeters: position.accuracy,
      );
    } on TimeoutException {
      return const LocationTimeout();
    } catch (_) {
      // geolocator throws PlatformException on some OEM quirks; degrade to a
      // typed failure rather than crashing the caller.
      return const LocationTimeout();
    }
  }

  /// Instant pin before the first fresh fix arrives. Returns a typed result so
  /// callers can distinguish "no last known" (failure) from a real position.
  Future<LocationResult> getLastKnownPosition() async {
    try {
      final position = await Geolocator.getLastKnownPosition();
      if (position == null) return const LocationTimeout();
      return LocationGranted(
        latitude: position.latitude,
        longitude: position.longitude,
        accuracyMeters: position.accuracy,
        isLastKnown: true,
      );
    } catch (_) {
      return const LocationTimeout();
    }
  }

  /// Continuous updates stream with configurable distance filter and interval.
  Stream<LatLng> watchPosition({
    double distanceFilterMeters = 10.0,
    Duration interval = const Duration(seconds: 3),
  }) {
    return Geolocator.getPositionStream(
      locationSettings: LocationSettings(
        accuracy: LocationAccuracy.high,
        distanceFilter: distanceFilterMeters.round(),
      ),
    ).map((p) => LatLng(p.latitude, p.longitude));
  }

  /// Permission state without triggering a prompt — used by the gate widget on
  /// resume-from-settings checks.
  Future<bool> hasPermission() async {
    final permission = await Geolocator.checkPermission();
    return permission == LocationPermission.whileInUse ||
        permission == LocationPermission.always;
  }
}
