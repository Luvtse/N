import 'package:latlong2/latlong.dart';

/// Which provider answered a geocoding request. Recorded for telemetry and for
/// the debug-drawer GeocodingHealth indicator (§1.3 step 7).
enum GeocodeProvider {
  nominatim('Nominatim'),
  photon('Photon'),
  device('Device geocoder'),
  cache('Local cache');

  final String label;
  const GeocodeProvider(this.label);
}

/// A single place-search suggestion returned by [GeocodingService.autocomplete].
///
/// Normalized across Nominatim / Photon / device-native responses so callers
/// never see provider-specific shapes.
class PlaceSuggestion {
  /// Stable identifier derived from coordinates + name; used as the cache key
  /// component and for list item keys in the UI.
  final String id;

  /// Primary display line (e.g. "Bole International Airport").
  final String name;

  /// Secondary display line (city / region / country, comma-joined).
  final String detail;

  final LatLng location;

  /// Meters from the search bias point, when a bias was supplied.
  final double? distanceMeters;

  /// True when this row came from the Hive cache because every network
  /// provider failed. The sheet renders a "showing cached results" note.
  final bool fromCache;

  const PlaceSuggestion({
    required this.id,
    required this.name,
    required this.detail,
    required this.location,
    this.distanceMeters,
    this.fromCache = false,
  });

  Map<String, dynamic> toJson() => {
        'id': id,
        'name': name,
        'detail': detail,
        'lat': location.latitude,
        'lng': location.longitude,
      };

  factory PlaceSuggestion.fromJson(Map<String, dynamic> json) =>
      PlaceSuggestion(
        id: json['id'] as String,
        name: json['name'] as String,
        detail: json['detail'] as String,
        location: LatLng(
          (json['lat'] as num).toDouble(),
          (json['lng'] as num).toDouble(),
        ),
      );
}

/// A resolved address from forward or reverse geocoding.
class GeocodeResult {
  final String displayAddress;
  final LatLng location;
  final GeocodeProvider provider;

  /// True when served from the local cache after all live providers failed.
  final bool fromCache;

  const GeocodeResult({
    required this.displayAddress,
    required this.location,
    required this.provider,
    this.fromCache = false,
  });
}

/// Raised when the entire provider chain (network + device + cache) fails.
/// Callers must surface an honest "Search unavailable" state — never invent a
/// result.
class GeocodingUnavailableException implements Exception {
  final String message;
  const GeocodingUnavailableException(
      [this.message = 'All geocoding providers are unavailable']);

  @override
  String toString() => 'GeocodingUnavailableException: $message';
}
