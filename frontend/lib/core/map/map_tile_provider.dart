import 'dart:async';
import 'dart:developer' as developer;
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter/foundation.dart';
import 'package:hive/hive.dart';
import 'package:latlong2/latlong.dart';

import 'map_health.dart';

/// Signature for the network fetch used by [MapTileProvider]. Injected so tests
/// can simulate mirror failures without real sockets.
typedef TileFetcher = Future<Uint8List?> Function(String url);

/// One entry in the ordered fallback chain.
class TileMirror {
  /// Stable name used in health reporting and telemetry (e.g. `primary`).
  final String name;

  /// URL template containing `{z}/{x}/{y}` placeholders, e.g.
  /// `https://tile.openstreetmap.org/{z}/{x}/{y}.png`.
  final String urlTemplate;

  const TileMirror({required this.name, required this.urlTemplate});
}

/// A tile provider implementing the mandated fallback chain:
///
///   primary mirror → secondary mirror → Hive tile cache (keyed by z/x/y) →
///   last-known-good raster tile.
///
/// It never throws to the caller. Every failure is surfaced through the
/// [health] stream instead, so the UI can show a MapHealth banner with retry.
class MapTileProvider {
  /// Ordered list of live mirrors. Index 0 is the primary.
  final List<TileMirror> mirrors;

  /// Optional Hive box used as the on-device tile cache. When null, the cache
  /// tier is skipped and only mirrors + in-memory last-known-good are used.
  final Box<List<int>>? tileCacheBox;

  /// Maximum number of tiles kept in the on-device cache (LRU eviction).
  final int maxCachedTiles;

  /// Consecutive full-chain network failures before health drops to
  /// [MapHealthStatus.offline].
  static const int _offlineThreshold = 3;

  final StreamController<MapHealth> _healthController =
      StreamController<MapHealth>.broadcast();

  final TileFetcher _fetch;

  int _consecutiveFailures = 0;
  String? _activeSource;
  MapHealthStatus _status = MapHealthStatus.healthy;

  // In-memory LRU of recently fetched tiles — the "last-known-good" tier that
  // survives even when the Hive box is not configured.
  final LinkedHashMap<String, Uint8List> _lastKnownGood =
      LinkedHashMap<String, Uint8List>();
  static const int _maxLastKnownGood = 256;

  MapTileProvider({
    required this.mirrors,
    required TileFetcher fetch,
    this.tileCacheBox,
    this.maxCachedTiles = 2000,
  })  : assert(mirrors.isNotEmpty, 'At least one tile mirror is required'),
        _fetch = fetch;

  /// The default dev/staging chain: OSM standard raster as primary, then a
  /// second public-style mirror slot. Production deployments must replace the
  /// templates with a self-hosted/commercial OSM mirror (OSM's public tile
  /// server forbids production traffic) — see
  /// doc/CONTRIBUTING-RIDE-PARITY.md rule 2.
  factory MapTileProvider.defaultChain({
    TileFetcher? fetch,
    Box<List<int>>? tileCacheBox,
  }) {
    return MapTileProvider(
      mirrors: const [
        TileMirror(
          name: 'primary',
          urlTemplate: 'https://tile.openstreetmap.org/{z}/{x}/{y}.png',
        ),
        TileMirror(
          name: 'secondary',
          urlTemplate: 'https://{s}.tile.openstreetmap.fr/osmfr/{z}/{x}/{y}.png',
        ),
      ],
      fetch: fetch ?? _httpFetch,
      tileCacheBox: tileCacheBox,
    );
  }

  /// Broadcast stream of tile-pipeline health. The UI subscribes to this to
  /// decide whether to render the MapHealth banner.
  Stream<MapHealth> get health => _healthController.stream;

  MapHealth get currentHealth => MapHealth(
        status: _status,
        activeSource: _activeSource,
        consecutiveFailures: _consecutiveFailures,
        timestamp: DateTime.now(),
      );

  /// Re-primes the provider after a user-visible "Retry": clears the failure
  /// counter and re-emits the current status so listeners re-evaluate. Called
  /// by the MapHealth banner's Retry button.
  void rePrime() {
    _consecutiveFailures = 0;
    _emit(_status, _activeSource);
  }

  /// Resolve bytes for one tile, walking the fallback chain. Returns null only
  /// when every tier failed — callers must treat null as "render nothing for
  /// this tile", never as an exception.
  Future<Uint8List?> getTile(TileCoords coords) async {
    final key = coords.toKey().storageKey;

    // Tier 1 & 2: live mirrors in declared order.
    for (final mirror in mirrors) {
      try {
        final bytes = await _fetch(mirror.urlTemplate
            .replaceAll('{z}', '${coords.z}')
            .replaceAll('{x}', '${coords.x}')
            .replaceAll('{y}', '${coords.y}')
            .replaceAll('{s}', 'a'));
        if (bytes != null && bytes.isNotEmpty) {
          _onSuccess(mirror.name);
          await _cacheTile(key, bytes);
          _rememberLastKnownGood(key, bytes);
          return bytes;
        }
      } catch (e, s) {
        debugPrint('MapTileProvider: ${mirror.name} failed for $key: $e');
        developer.log('${mirror.name} failure', name: 'map.tiles', error: e, stackTrace: s);
      }
    }

    // Tier 3: Hive cache keyed by z/x/y.
    final cached = await _readCachedTile(key);
    if (cached != null) {
      _onCacheHit();
      return cached;
    }

    // Tier 4: last-known-good in-memory copy.
    final lkg = _lastKnownGood[key];
    if (lkg != null) {
      _onCacheHit();
      return lkg;
    }

    // Nothing left — report, never throw.
    _onNetworkFailure();
    return null;
  }

  /// Convenience for camera-fit math consumers.
  static LatLngBoundsLike boundsOf(Iterable<LatLng> points) =>
      LatLngBoundsLike.fromPoints(points);

  Future<void> dispose() async {
    await _healthController.close();
  }

  // --------------------------------------------------------------------------
  // Internals
  // --------------------------------------------------------------------------

  Future<void> _cacheTile(String key, Uint8List bytes) async {
    final box = tileCacheBox;
    if (box == null) return;
    try {
      if (box.length >= maxCachedTiles) {
        // Approximate LRU: drop the oldest insertion-order keys.
        final excess = box.length - maxCachedTiles + 1;
        for (var i = 0; i < excess && box.isNotEmpty; i++) {
          await box.delete(box.keys.first);
        }
      }
      await box.put(key, bytes);
    } catch (e) {
      debugPrint('MapTileProvider: cache write failed: $e');
    }
  }

  Future<Uint8List?> _readCachedTile(String key) async {
    final box = tileCacheBox;
    if (box == null) return null;
    try {
      final raw = box.get(key);
      if (raw == null) return null;
      return Uint8List.fromList(raw);
    } catch (e) {
      debugPrint('MapTileProvider: cache read failed: $e');
      return null;
    }
  }

  void _rememberLastKnownGood(String key, Uint8List bytes) {
    _lastKnownGood.remove(key);
    _lastKnownGood[key] = bytes;
    while (_lastKnownGood.length > _maxLastKnownGood) {
      _lastKnownGood.remove(_lastKnownGood.keys.first);
    }
  }

  void _onSuccess(String sourceName) {
    _consecutiveFailures = 0;
    final status = sourceName == mirrors.first.name
        ? MapHealthStatus.healthy
        : MapHealthStatus.degraded;
    _emit(status, sourceName);
  }

  void _onCacheHit() {
    _emit(MapHealthStatus.offline, 'device-cache');
  }

  void _onNetworkFailure() {
    _consecutiveFailures++;
    final status = _consecutiveFailures >= _offlineThreshold
        ? MapHealthStatus.totalFailure
        : MapHealthStatus.offline;
    _emit(status, null);
  }

  void _emit(MapHealthStatus status, String? source) {
    if (status == _status && source == _activeSource) return;
    _status = status;
    _activeSource = source;
    if (!_healthController.isClosed) {
      _healthController.add(MapHealth(
        status: status,
        activeSource: source,
        consecutiveFailures: _consecutiveFailures,
        timestamp: DateTime.now(),
      ));
    }
  }

  static Future<Uint8List?> _httpFetch(String url) async {
    final client = HttpClient()..userAgent = 'NidusApp/1.0 (ride-hailing client)';
    try {
      final request = await client.getUrl(Uri.parse(url));
      final response = await request.close().timeout(const Duration(seconds: 10));
      if (response.statusCode != HttpStatus.ok) return null;
      final builder = await response.fold(
        BytesBuilder(copy: false),
        (BytesBuilder b, List<int> chunk) => b..add(chunk),
      );
      return builder.takeBytes();
    } catch (e) {
      debugPrint('MapTileProvider: fetch error for $url: $e');
      return null;
    } finally {
      client.close(force: true);
    }
  }
}
