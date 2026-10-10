import 'dart:async';

import 'package:latlong2/latlong.dart';

/// Health states reported by [MapTileProvider] through the [MapHealth] stream.
enum MapHealthStatus {
  /// The primary tile mirror is answering requests.
  healthy,

  /// The primary mirror failed; a secondary mirror or cache is serving tiles.
  degraded,

  /// Every live mirror failed; tiles are being served only from on-device
  /// cache (last-known-good). The UI should surface this with a retry action.
  offline,

  /// No tiles can be produced at all (cache empty and every mirror down).
  totalFailure,
}

/// A single health snapshot of the tile pipeline.
class MapHealth {
  final MapHealthStatus status;

  /// Human-readable name of the source currently answering
  /// (e.g. `primary`, `secondary`, `hive-cache`). Null when [status] is
  /// [MapHealthStatus.totalFailure].
  final String? activeSource;

  /// Number of consecutive network failures observed across all mirrors.
  final int consecutiveFailures;

  /// Wall-clock time this snapshot was produced.
  final DateTime timestamp;

  const MapHealth({
    required this.status,
    required this.activeSource,
    required this.consecutiveFailures,
    required this.timestamp,
  });

  factory MapHealth.healthy() => MapHealth(
        status: MapHealthStatus.healthy,
        activeSource: 'primary',
        consecutiveFailures: 0,
        timestamp: DateTime.now(),
      );

  bool get isFullyBroken => status == MapHealthStatus.totalFailure;
}

/// Sink used by [MapTileProvider] to publish health updates. Exposed so tests
/// and the provider itself share one definition.
typedef MapHealthSink = void Function(MapHealth health);

/// Key identifying one raster tile in the on-device cache: z/x/y.
class TileKey {
  final int z;
  final int x;
  final int y;

  const TileKey(this.z, this.x, this.y);

  /// Hive/cache key string, e.g. `tile/13/4258/2736`.
  String get storageKey => 'tile/$z/$x/$y';

  @override
  bool operator ==(Object other) =>
      other is TileKey && other.z == z && other.x == x && other.y == y;

  @override
  int get hashCode => Object.hash(z, x, y);

  @override
  String toString() => 'TileKey($z/$x/$y)';
}

/// Resolved coordinates for a tile request.
class TileCoords {
  final int z;
  final int x;
  final int y;

  const TileCoords(this.z, this.x, this.y);

  TileKey toKey() => TileKey(z, x, y);

  /// Fills a `{z}/{x}/{y}` URL template.
  String fill(String template) => template
      .replaceAll('{z}', '$z')
      .replaceAll('{x}', '$x')
      .replaceAll('{y}', '$y');
}

/// Simple bounding box used by the camera-fit helpers in LiveMapWidget.
class LatLngBoundsLike {
  final LatLng southWest;
  final LatLng northEast;

  const LatLngBoundsLike(this.southWest, this.northEast);

  factory LatLngBoundsLike.fromPoints(Iterable<LatLng> points) {
    double minLat = 90, maxLat = -90, minLng = 180, maxLng = -180;
    for (final p in points) {
      if (p.latitude < minLat) minLat = p.latitude;
      if (p.latitude > maxLat) maxLat = p.latitude;
      if (p.longitude < minLng) minLng = p.longitude;
      if (p.longitude > maxLng) maxLng = p.longitude;
    }
    return LatLngBoundsLike(
      LatLng(minLat, minLng),
      LatLng(maxLat, maxLng),
    );
  }
}
