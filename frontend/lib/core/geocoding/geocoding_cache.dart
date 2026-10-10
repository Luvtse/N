import 'package:hive/hive.dart';
import 'package:latlong2/latlong.dart';

import 'geocoding_models.dart';

/// Hive-backed cache for geocoding results (§1.3 step 3).
///
/// TTLs are locked by the plan: 7 days for autocomplete queries, 30 days for
/// reverse-geocode results. A size cap with LRU eviction keeps the box bounded
/// on-device. When every network provider fails, cached autocomplete rows are
/// returned with [PlaceSuggestion.fromCache] = true so the UI can honestly say
/// "showing cached results".
class GeocodingCache {
  static const String boxName = 'geocoding_cache';

  /// Max live entries before LRU eviction kicks in.
  static const int maxEntries = 500;

  static const Duration autocompleteTtl = Duration(days: 7);
  static const Duration reverseTtl = Duration(days: 30);

  static const String _kindAutocomplete = 'ac';
  static const String _kindReverse = 'rv';

  final Box<dynamic> _box;

  GeocodingCache(this._box);

  /// Opens (or creates) the underlying Hive box. Call after
  /// `Hive.initFlutter()` during app bootstrap.
  static Future<GeocodingCache> open() async {
    final box = await Hive.openBox<dynamic>(boxName);
    return GeocodingCache(box);
  }

  static String normalizeQuery(String query) =>
      query.trim().toLowerCase().replaceAll(RegExp(r'\s+'), ' ');

  String _autocompleteKey(String query, {String? biasKey}) =>
      '$_kindAutocomplete|${normalizeQuery(query)}'
      '${biasKey == null ? '' : '|$biasKey'}';

  String _reverseKey(double lat, double lng) =>
      '$_kindReverse|${lat.toStringAsFixed(4)},${lng.toStringAsFixed(4)}';

  /// Cached suggestions for a previously-searched query, or null when absent
  /// or expired. Marks the entry as recently used on hit (LRU bookkeeping).
  List<PlaceSuggestion>? getAutocomplete(String query, {String? biasKey}) {
    final key = _autocompleteKey(query, biasKey: biasKey);
    final raw = _box.get(key);
    if (raw == null) return null;
    final entry = _CacheEntry.fromJson(Map<String, dynamic>.from(raw as Map));
    if (_isExpired(entry, autocompleteTtl)) {
      _box.delete(key);
      return null;
    }
    // Touch lastAccessed so eviction prefers stale entries.
    _box.put(key, {...raw as Map, 'accessed': DateTime.now().toIso8601String()});
    return entry.items
        .map((j) => PlaceSuggestion.fromJson(Map<String, dynamic>.from(j)))
        .toList();
  }

  Future<void> putAutocomplete(
    String query,
    List<PlaceSuggestion> results, {
    String? biasKey,
  }) async {
    final key = _autocompleteKey(query, biasKey: biasKey);
    final entry = _CacheEntry(
      stored: DateTime.now().toIso8601String(),
      accessed: DateTime.now().toIso8601String(),
      items: results.map((s) => s.toJson()).toList(),
    );
    await _box.put(key, entry.toJson());
    await _evictIfNeeded();
  }

  GeocodeResult? getReverse(double lat, double lng) {
    final key = _reverseKey(lat, lng);
    final raw = _box.get(key);
    if (raw == null) return null;
    final entry = _CacheEntry.fromJson(Map<String, dynamic>.from(raw as Map));
    if (_isExpired(entry, reverseTtl)) {
      _box.delete(key);
      return null;
    }
    final item = entry.items.isEmpty ? null : entry.items.first;
    if (item == null) return null;
    final json = Map<String, dynamic>.from(item);
    return GeocodeResult(
      displayAddress: json['display'] as String,
      location: LatLng(
        (json['lat'] as num).toDouble(),
        (json['lng'] as num).toDouble(),
      ),
      provider: GeocodeProvider.cache,
      fromCache: true,
    );
  }

  Future<void> putReverse(GeocodeResult result) async {
    final key = _reverseKey(result.location.latitude, result.location.longitude);
    final entry = _CacheEntry(
      stored: DateTime.now().toIso8601String(),
      accessed: DateTime.now().toIso8601String(),
      items: [
        {
          'display': result.displayAddress,
          'lat': result.location.latitude,
          'lng': result.location.longitude,
        }
      ],
    );
    await _box.put(key, entry.toJson());
    await _evictIfNeeded();
  }

  bool _isExpired(_CacheEntry e, Duration ttl) {
    final stored = DateTime.tryParse(e.stored);
    if (stored == null) return true;
    return DateTime.now().difference(stored) > ttl;
  }

  /// LRU eviction: when over [maxEntries], drop the least-recently-accessed
  /// entries until back under the cap.
  Future<void> _evictIfNeeded() async {
    if (_box.length <= maxEntries) return;
    final entries = _box.toMap().entries.toList();
    entries.sort((a, b) {
      final ta = _accessTime(a.value);
      final tb = _accessTime(b.value);
      return ta.compareTo(tb);
    });
    final overflow = entries.length - maxEntries;
    for (var i = 0; i < overflow; i++) {
      await _box.delete(entries[i].key);
    }
  }

  DateTime _accessTime(dynamic raw) {
    if (raw is! Map) return DateTime.fromMillisecondsSinceEpoch(0);
    return DateTime.tryParse('${raw['accessed']}') ??
        DateTime.fromMillisecondsSinceEpoch(0);
  }

  Future<void> clear() => _box.clear();

  int get length => _box.length;
}

class _CacheEntry {
  final String stored;
  final String accessed;
  final List<dynamic> items;

  _CacheEntry({
    required this.stored,
    required this.accessed,
    required this.items,
  });

  factory _CacheEntry.fromJson(Map<String, dynamic> json) => _CacheEntry(
        stored: json['stored'] as String,
        accessed: json['accessed'] as String,
        items: (json['items'] as List).cast<dynamic>(),
      );

  Map<String, dynamic> toJson() => {
        'stored': stored,
        'accessed': accessed,
        'items': items,
      };
}
