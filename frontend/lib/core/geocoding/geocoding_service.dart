import 'dart:async';
import 'dart:math' as math;

import 'package:dio/dio.dart';
import 'package:latlong2/latlong.dart';

import 'geocoding_cache.dart';
import 'geocoding_models.dart';
import 'geocoding_rate_limiter.dart';

/// Streams the last provider that answered a geocoding request so ops/debug UI
/// can watch the fallback chain work (§1.3 step 7 — GeocodingHealth).
class GeocodingHealth {
  final _controller = StreamController<GeocodeProvider>.broadcast();
  GeocodeProvider? _last;

  Stream<GeocodeProvider> get stream => _controller.stream;
  GeocodeProvider? get lastProvider => _last;

  void record(GeocodeProvider provider) {
    _last = provider;
    _controller.add(provider);
  }

  void dispose() => _controller.close();
}

/// Real, resilient place search with the locked provider chain (§1.3):
///
///   Nominatim (primary) → Photon (fallback 1) → device-native geocoder
///   (fallback 2, injected) → Hive cache (fallback 3).
///
/// Every response is normalized into [PlaceSuggestion] / [GeocodeResult]; the
/// provider that answered is recorded on [health]. Nominatim traffic is capped
/// at 1 req/sec by [GeocodingRateLimiter] and identified by a real User-Agent
/// per its usage policy. Nothing here ever returns fabricated data: if the
/// whole chain fails, callers receive a [GeocodingUnavailableException] and
/// must render an honest "unavailable" state.
class GeocodingService {
  static const String userAgent =
      'NidusApp/1.0 (ride-hailing client; contact=support@nidus.app)';

  static const String nominatimSearchUrl =
      'https://nominatim.openstreetmap.org/search';
  static const String nominatimReverseUrl =
      'https://nominatim.openstreetmap.org/reverse';
  static const String photonSearchUrl = 'https://photon.komoot.io/api/';
  static const String photonReverseUrl = 'https://photon.komoot.io/reverse';

  static const Duration _httpTimeout = Duration(seconds: 8);

  final Dio _dio;
  final GeocodingCache _cache;
  final GeocodingRateLimiter _nominatimLimiter;
  final GeocodingHealth health;

  /// Device-native geocoder hook (Android Geocoder / iOS CLGeocoder via the
  /// `geocoding` package). Injected so this service stays testable and so the
  /// plugin import lives in exactly one composition-root file.
  final Future<List<PlaceSuggestion>> Function(String query, LatLng? bias)?
      deviceAutocomplete;
  final Future<GeocodeResult?> Function(LatLng point)? deviceReverse;

  GeocodingService({
    required Dio dio,
    required GeocodingCache cache,
    GeocodingRateLimiter? nominatimLimiter,
    GeocodingHealth? health,
    this.deviceAutocomplete,
    this.deviceReverse,
  })  : _dio = dio,
        _cache = cache,
        _nominatimLimiter = nominatimLimiter ?? GeocodingRateLimiter(),
        health = health ?? GeocodingHealth();

  // ---- public API -----------------------------------------------------------

  /// Place autocomplete. Walks the provider chain; debouncing (300 ms) belongs
  /// to the UI layer, rate limiting (1 rps vs Nominatim) is enforced here
  /// (defense in depth).
  Future<List<PlaceSuggestion>> autocomplete(
    String query, {
    LatLng? bias,
    int limit = 7,
  }) async {
    final trimmed = query.trim();
    if (trimmed.isEmpty) return const [];

    final biasKey = bias == null
        ? null
        : '${bias.latitude.toStringAsFixed(4)},'
            '${bias.longitude.toStringAsFixed(4)}';

    List<PlaceSuggestion>? results;

    // Primary: Nominatim (rate-limited, honest User-Agent).
    try {
      await _nominatimLimiter.acquire();
      results = await _nominatimSearch(trimmed, bias: bias, limit: limit);
      if (results.isNotEmpty) {
        health.record(GeocodeProvider.nominatim);
      }
    } on Object {
      results = null;
    }

    // Fallback 1: Photon.
    if (results == null || results.isEmpty) {
      try {
        results = await _photonSearch(trimmed, bias: bias, limit: limit);
        if (results.isNotEmpty) health.record(GeocodeProvider.photon);
      } on Object {
        results = null;
      }
    }

    // Fallback 2: device-native geocoder.
    if ((results == null || results.isEmpty) && deviceAutocomplete != null) {
      try {
        results = await deviceAutocomplete!(trimmed, bias);
        if (results != null && results.isNotEmpty) {
          health.record(GeocodeProvider.device);
        }
      } on Object {
        results = null;
      }
    }

    if (results != null && results.isNotEmpty) {
      final biased = _withDistances(results, bias);
      unawaited(_cache.putAutocomplete(trimmed, biased, biasKey: biasKey));
      return biased;
    }

    // Fallback 3: cached rows, flagged so the sheet can say
    // "Search unavailable — showing cached results".
    final cached = _cache.getAutocomplete(trimmed, biasKey: biasKey);
    if (cached != null && cached.isNotEmpty) {
      health.record(GeocodeProvider.cache);
      return cached
          .map((s) => PlaceSuggestion(
                id: s.id,
                name: s.name,
                detail: s.detail,
                location: s.location,
                distanceMeters:
                    bias == null ? null : _metersBetween(s.location, bias),
                fromCache: true,
              ))
          .take(limit)
          .toList();
    }

    throw const GeocodingUnavailableException();
  }

  /// Reverse geocode a map coordinate into a human address label. Used by the
  /// pickup-pin drag (debounced 500 ms in the UI).
  Future<GeocodeResult> reverse(LatLng point) async {
    // Nominatim first.
    try {
      await _nominatimLimiter.acquire();
      final resp = await _dio.get<dynamic>(
        nominatimReverseUrl,
        queryParameters: {
          'lat': point.latitude,
          'lon': point.longitude,
          'format': 'jsonv2',
          'zoom': 18,
          'addressdetails': 1,
        },
        options: _options(),
      );
      final json = resp.data;
      if (json is Map && json['display_name'] is String) {
        final result = GeocodeResult(
          displayAddress: json['display_name'] as String,
          location: point,
          provider: GeocodeProvider.nominatim,
        );
        health.record(GeocodeProvider.nominatim);
        unawaited(_cache.putReverse(result));
        return result;
      }
    } on Object {
      // fall through
    }

    // Photon.
    try {
      final resp = await _dio.get<dynamic>(
        photonReverseUrl,
        queryParameters: {'lat': point.latitude, 'lon': point.longitude},
        options: _options(),
      );
      final feature = _firstPhotonFeature(resp.data);
      if (feature != null) {
        final result = GeocodeResult(
          displayAddress: _photonDisplayName(feature),
          location: point,
          provider: GeocodeProvider.photon,
        );
        health.record(GeocodeProvider.photon);
        unawaited(_cache.putReverse(result));
        return result;
      }
    } on Object {
      // fall through
    }

    // Device-native.
    if (deviceReverse != null) {
      try {
        final result = await deviceReverse!(point);
        if (result != null) {
          health.record(GeocodeProvider.device);
          unawaited(_cache.putReverse(result));
          return result;
        }
      } on Object {
        // fall through
      }
    }

    // Cache (30-day TTL) — honest fromCache flag.
    final cached = _cache.getReverse(point.latitude, point.longitude);
    if (cached != null) {
      health.record(GeocodeProvider.cache);
      return cached;
    }

    throw const GeocodingUnavailableException('reverse geocode failed');
  }

  /// Forward geocode a typed address string. Takes the first best match from
  /// the same chain used by [autocomplete].
  Future<GeocodeResult> forward(String address) async {
    final suggestions = await autocomplete(address, limit: 1);
    if (suggestions.isEmpty) {
      throw const GeocodingUnavailableException('no match for address');
    }
    final s = suggestions.first;
    return GeocodeResult(
      displayAddress: s.detail.isEmpty ? s.name : '${s.name}, ${s.detail}',
      location: s.location,
      provider: health.lastProvider ?? GeocodeProvider.nominatim,
      fromCache: s.fromCache,
    );
  }

  // ---- providers ------------------------------------------------------------

  Options _options() => Options(
        headers: {'User-Agent': userAgent},
        responseType: ResponseType.json,
        sendTimeout: _httpTimeout,
        receiveTimeout: _httpTimeout,
      );

  Future<List<PlaceSuggestion>> _nominatimSearch(
    String query, {
    required LatLng? bias,
    required int limit,
  }) async {
    final resp = await _dio.get<dynamic>(
      nominatimSearchUrl,
      queryParameters: {
        'q': query,
        'format': 'jsonv2',
        'limit': limit,
        'addressdetails': 1,
        if (bias != null) ...{
          'viewbox': '${bias.longitude - 0.05},${bias.latitude + 0.05},'
              '${bias.longitude + 0.05},${bias.latitude - 0.05}',
          'bounded': 0,
        },
      },
      options: _options(),
    );
    final data = resp.data;
    if (data is! List) return const [];
    return data.whereType<Map>().map((item) {
      final lat = double.tryParse('${item['lat']}');
      final lon = double.tryParse('${item['lon']}');
      if (lat == null || lon == null) return null;
      final displayName = '${item['display_name'] ?? ''}';
      final parts = displayName.split(', ');
      final name = parts.length > 1 ? parts.first : displayName;
      final detail = parts.length > 1 ? parts.skip(1).join(', ') : '';
      if (name.trim().isEmpty) return null;
      return PlaceSuggestion(
        id: 'nom|$lat|$lon|${item['osm_id'] ?? name}',
        name: name,
        detail: detail,
        location: LatLng(lat, lon),
      );
    }).whereType<PlaceSuggestion>().toList();
  }

  Future<List<PlaceSuggestion>> _photonSearch(
    String query, {
    required LatLng? bias,
    required int limit,
  }) async {
    final resp = await _dio.get<dynamic>(
      photonSearchUrl,
      queryParameters: {
        'q': query,
        'limit': limit,
        if (bias != null) ...{'lat': bias.latitude, 'lon': bias.longitude},
      },
      options: _options(),
    );
    final features =
        (resp.data is Map) ? (resp.data as Map)['features'] : null;
    if (features is! List) return const [];
    return features.whereType<Map>().map((f) {
      final geom = f['geometry'];
      if (geom is! Map) return null;
      final coords = geom['coordinates'];
      if (coords is! List || coords.length < 2) return null;
      final lon = (coords[0] as num).toDouble();
      final lat = (coords[1] as num).toDouble();
      final props = (f['properties'] as Map?) ?? const {};
      final name = '${props['name'] ?? props['street'] ?? ''}';
      final city = '${props['city'] ?? props['state'] ?? ''}';
      final country = '${props['country'] ?? ''}';
      final detail =
          [city, country].where((s) => s.trim().isNotEmpty).join(', ');
      if (name.trim().isEmpty) return null;
      return PlaceSuggestion(
        id: 'pho|$lat|$lon|$name',
        name: name,
        detail: detail,
        location: LatLng(lat, lon),
      );
    }).whereType<PlaceSuggestion>().toList();
  }

  Map _firstPhotonFeature(dynamic data) {
    if (data is Map && data['features'] is List) {
      final list = (data['features'] as List).whereType<Map>().toList();
      if (list.isNotEmpty) return list.first;
    }
    return {};
  }

  String _photonDisplayName(Map feature) {
    final p = (feature['properties'] as Map?) ?? const {};
    final name = '${p['name'] ?? p['street'] ?? ''}';
    final city = '${p['city'] ?? p['town'] ?? p['village'] ?? ''}';
    final country = '${p['country'] ?? ''}';
    return [name, city, country]
        .where((s) => s.trim().isNotEmpty)
        .join(', ');
  }

  // ---- geometry helpers -----------------------------------------------------

  List<PlaceSuggestion> _withDistances(
    List<PlaceSuggestion> suggestions,
    LatLng? bias,
  ) {
    if (bias == null) return suggestions;
    return suggestions
        .map((s) => PlaceSuggestion(
              id: s.id,
              name: s.name,
              detail: s.detail,
              location: s.location,
              distanceMeters: _metersBetween(s.location, bias),
              fromCache: s.fromCache,
            ))
        .toList();
  }

  /// Haversine distance in meters.
  static double _metersBetween(LatLng a, LatLng b) {
    const earthRadiusM = 6371000.0;
    double rad(double deg) => deg * math.pi / 180.0;
    final dLat = rad(b.latitude - a.latitude);
    final dLon = rad(b.longitude - a.longitude);
    final h = math.sin(dLat / 2) * math.sin(dLat / 2) +
        math.cos(rad(a.latitude)) *
            math.cos(rad(b.latitude)) *
            math.sin(dLon / 2) *
            math.sin(dLon / 2);
    return 2 * earthRadiusM * math.atan2(math.sqrt(h), math.sqrt(1 - h));
  }
}
