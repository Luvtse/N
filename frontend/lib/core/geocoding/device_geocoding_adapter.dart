import 'dart:math' as _dmath;

import 'package:geocoding/geocoding.dart' as native;
import 'package:latlong2/latlong.dart';

import 'geocoding_models.dart';

/// Device-native geocoder adapter (Fallback 2 in the §1.3 chain).
///
/// Wraps the `geocoding` package (Android Geocoder / iOS CLGeocoder) so the
/// phone's own chips do coarse resolution work for free — and offline on many
/// Android builds. Injected into [GeocodingService] at the composition root so
/// this plugin import lives in exactly one file besides this adapter.
class DeviceGeocodingAdapter {
  /// Autocomplete via device lookup. The native geocoder is not a true search
  /// engine, so results are coarse; that is acceptable because it only answers
  /// when both network providers failed.
  static Future<List<PlaceSuggestion>> autocomplete(
    String query,
    LatLng? bias,
  ) async {
    final places = await native.locationFromAddress(query);
    if (places.isEmpty) return const [];
    final origin = bias ?? LatLng(places.first.latitude, places.first.longitude);
    final suggestions = <PlaceSuggestion>[];
    for (final p in places.take(7)) {
      final addresses =
          await native.placemarkFromCoordinates(p.latitude, p.longitude);
      final detail = addresses.isEmpty
          ? ''
          : _joinParts([
              addresses.first.subLocality,
              addresses.first.locality,
              addresses.first.administrativeArea,
              addresses.first.country,
            ]);
      suggestions.add(PlaceSuggestion(
        id: 'dev|${p.latitude}|${p.longitude}',
        name: query,
        detail: detail,
        location: LatLng(p.latitude, p.longitude),
        distanceMeters: _metersBetween(origin, LatLng(p.latitude, p.longitude)),
      ));
    }
    return suggestions;
  }

  /// Reverse geocode using the device placemark API. Returns null when the
  /// platform yields nothing so the chain can fall through to cache — never a
  /// fabricated label.
  static Future<GeocodeResult?> reverse(LatLng point) async {
    try {
      final placemarks = await native.placemarkFromCoordinates(
        point.latitude,
        point.longitude,
      );
      if (placemarks.isEmpty) return null;
      final pm = placemarks.first;
      final line = _joinParts([
        pm.name,
        pm.thoroughfare,
        pm.subLocality,
        pm.locality,
        pm.administrativeArea,
        pm.country,
      ]);
      if (line.isEmpty) return null;
      return GeocodeResult(
        displayAddress: line,
        location: point,
        provider: GeocodeProvider.device,
      );
    } on Object {
      // Platform channel failure (e.g. no geocoder backend on some emulators):
      // honest null so the chain falls through instead of faking a label.
      return null;
    }
  }

  static String _joinParts(List<String?> parts) => parts
      .whereType<String>()
      .where((s) => s.trim().isNotEmpty)
      .join(', ');

  static double _metersBetween(LatLng a, LatLng b) {
    const earthRadiusM = 6371000.0;
    double rad(double deg) => deg * math_pi / 180.0;
    final dLat = rad(b.latitude - a.latitude);
    final dLon = rad(b.longitude - a.longitude);
    final sinHalfLat = _sin(dLat / 2);
    final sinHalfLon = _sin(dLon / 2);
    final h = sinHalfLat * sinHalfLat +
        _cos(rad(a.latitude)) * _cos(rad(b.latitude)) * sinHalfLon * sinHalfLon;
    return 2 * earthRadiusM * _atan2(_sqrt(h), _sqrt(1 - h));
  }
}

// dart:math imported under aliases so nothing here shadows Flutter symbols.
const double math_pi = 3.141592653589793;
double _sin(double x) => _dmath.sin(x);
double _cos(double x) => _dmath.cos(x);
double _sqrt(double x) => _dmath.sqrt(x);
double _atan2(double y, double x) => _dmath.atan2(y, x);
