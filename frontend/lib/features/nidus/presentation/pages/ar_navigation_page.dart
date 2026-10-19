import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:geolocator/geolocator.dart';
import 'package:latlong2/latlong.dart';
import 'package:vector_math/vector_math_64.dart' as vector_math;

// Imported without a prefix so the plugin's top-level `namespace` constant
// does not collide with dart:core's Type.namespace member (a prefixed import
// would otherwise force `prefix.namespace` to resolve against the wrong
// symbol and fail analysis). All plugin types used below are uniquely named.
import 'package:ar_flutter_plugin/ar_flutter_plugin.dart';
import 'package:ar_flutter_plugin/datatypes/node_types.dart';
import 'package:ar_flutter_plugin/managers/ar_anchor_manager.dart';
import 'package:ar_flutter_plugin/managers/ar_location_manager.dart';
import 'package:ar_flutter_plugin/managers/ar_object_manager.dart';
import 'package:ar_flutter_plugin/managers/ar_session_manager.dart';
import 'package:ar_flutter_plugin/models/ar_node.dart';

import '../../../../core/location/device_location_service.dart';
import '../../../../core/map/live_map_widget.dart';
import '../../../../core/theme/app_colors.dart';

/// §4.1 AR walk-to-pickup surface.
///
/// Renders the live camera AR view when the device supports it; whenever
/// ARCore/ARKit is unavailable (unsupported hardware, missing Play Services,
/// session-init failure discovered late) it falls back to a real 2D
/// [LiveMapWidget] showing the rider's GPS position, the pickup pin and a
/// walking line between them — never a blank screen, never an "unsupported"
/// dead-end. The bottom "Map View"/"AR View" button toggles the two modes at
/// any time (§4.1 requirement that the AR page uses the same LiveMapWidget
/// for its 2D fallback).
///
/// Asset note: the AR node models (`assets/models/arrow.glb`,
/// `assets/models/distance_marker.glb`) ship only when present in the asset
/// manifest. When absent, the AR branch renders a compass-bearing chevron
/// overlay instead of pointing the plugin at missing files (Global Rule: no
/// placeholders on shipped surfaces).
class ARNavigationPage extends StatefulWidget {
  final double destinationLat;
  final double destinationLng;
  final String destinationName;

  const ARNavigationPage({
    super.key,
    required this.destinationLat,
    required this.destinationLng,
    required this.destinationName,
  });

  @override
  State<ARNavigationPage> createState() => _ARNavigationPageState();
}

class _ARNavigationPageState extends State<ARNavigationPage> {
  /// Distance filter for the walking-navigation position stream (meters).
  static const double _positionDistanceFilter = 5.0;

  final DeviceLocationService _locationService =
      const DeviceLocationService();
  final GlobalKey<LiveMapWidgetState> _mapKey =
      GlobalKey<LiveMapWidgetState>();

  StreamSubscription<LatLng>? _positionSub;
  StreamSubscription<double>? _headingSub;

  LatLng? _currentPosition;

  /// null while the platform has no heading source (emulator, no magnetometer
  /// permission); the header then shows the computed bearing instead of a
  /// fabricated heading.
  double? _heading;
  double _distanceToDestination = double.nan;

  /// null = still probing; false = AR unsupported (2D fallback);
  /// true = probe passed (runtime failures flip [_arRenderFailed]).
  bool? _arSupported;

  /// True once the AR view signalled an error after the initial probe — the
  /// page swaps to the 2D fallback permanently for this visit.
  bool _arRenderFailed = false;

  /// Explicit user toggle driven by the Map View / AR View button.
  bool _show2DFallback = false;

  /// glTF arrow/marker models confirmed present in the asset bundle.
  bool _arModelsPresent = false;

  @override
  void initState() {
    super.initState();
    unawaited(_probeAssets());
    unawaited(_startLocationUpdates());
    WidgetsBinding.instance.addPostFrameCallback((_) => _probeARSupport());
  }

  Future<void> _probeAssets() async {
    var present = false;
    try {
      final manifest = await AssetManifest.loadFromAssetBundle(rootBundle);
      present = manifest
              .getList()
              .whereType<String>()
              .contains('assets/models/arrow.glb') ||
          manifest.listAssets().any((a) => a.startsWith('assets/models/'));
    } catch (_) {
      // Bundle unreadable (tests): assume absent → bearing overlay path.
      present = false;
    }
    if (!mounted) return;
    setState(() => _arModelsPresent = present);
  }

  /// Probes AR support through the plugin's own availability call. Any throw
  /// or negative answer flips the page to the 2D fallback; there is no code
  /// path where an unsupported device sees a black camera rectangle.
  ///
  /// The probe goes through a guarded MethodChannel rather than a typed
  /// plugin symbol: `ar_flutter_plugin`'s Dart API surface (ArCoreController /
  /// ArCoreNode) exposes no static `isSupported()` in ^0.7.x, and depending on
  /// one that may not exist would be an orphan-import class of bug (Global
  /// Rule 3). The native side of every supported platform registers the
  /// 'ar_flutter_plugin' channel; a missing channel (desktop, tests, iOS
  /// without ARKit wiring) or an unimplemented/failed check all resolve to
  /// "not supported", which routes to the LiveMapWidget 2D fallback.
  Future<void> _probeARSupport() async {
    bool supported = false;
    try {
      const channel = MethodChannel('ar_flutter_plugin');
      final result = await channel
          .invokeMethod<bool>('isSupported')
          .timeout(const Duration(seconds: 3));
      supported = result ?? false;
    } on MissingPluginException {
      supported = false;
    } on PlatformException {
      supported = false;
    } on TimeoutException {
      supported = false;
    } catch (_) {
      supported = false;
    }
    if (!mounted) return;
    setState(() => _arSupported = supported);
  }

  Future<void> _startLocationUpdates() async {
    // Seed an instant pin from the last known fix before the first fresh one.
    final lastKnown = await _locationService.getLastKnownPosition();
    if (lastKnown is LocationGranted && mounted) {
      setState(() {
        _currentPosition = lastKnown.toLatLng();
        _recomputeNavigation();
      });
    }

    _positionSub = _locationService
        .watchPosition(distanceFilterMeters: _positionDistanceFilter)
        .listen(
      (point) {
        if (!mounted) return;
        setState(() {
          _currentPosition = point;
          _recomputeNavigation();
        });
      },
      onError: (_) {
        // GPS disabled mid-session: keep the last-known pin frozen. The
        // header keeps reporting the distance measured from that fix, which
        // is honest until a newer one exists.
      },
    );

    try {
      _headingSub = Geolocator.getHeadingStream().listen(
        (value) {
          if (!mounted) return;
          setState(() => _heading = value);
        },
        onError: (_) {
          _safeSetState(() => _heading = null);
        },
      );
    } catch (_) {
      // Platform without heading support: bearing-only guidance.
      _headingSub = null;
      _heading = null;
    }
  }

  void _safeSetState(VoidCallback fn) {
    if (mounted) setState(fn);
  }

  void _recomputeNavigation() {
    final pos = _currentPosition;
    if (pos == null) return;
    _distanceToDestination = Geolocator.distanceBetween(
      pos.latitude,
      pos.longitude,
      widget.destinationLat,
      widget.destinationLng,
    );
  }

  double get _bearingToDestination {
    final pos = _currentPosition;
    if (pos == null) return double.nan;
    return Geolocator.bearingBetween(
      pos.latitude,
      pos.longitude,
      widget.destinationLat,
      widget.destinationLng,
    );
  }

  String get _distanceLabel {
    if (_currentPosition == null || !_distanceToDestination.isFinite) {
      return 'Locating…';
    }
    if (_distanceToDestination < 1000) {
      return '${_distanceToDestination.toStringAsFixed(0)} m';
    }
    return '${(_distanceToDestination / 1000).toStringAsFixed(1)} km';
  }

  bool get _useFallback =>
      _arSupported != true || _arRenderFailed || _show2DFallback;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: Colors.black,
      body: Stack(
        children: [
          Positioned.fill(child: _buildPrimaryView()),
          _buildHeader(),
          _buildControls(),
        ],
      ),
    );
  }

  Widget _buildPrimaryView() {
    // Probing state renders the 2D map immediately (never a blank screen);
    // once the probe confirms support we swap to the live AR scene.
    if (_useFallback) {
      return _build2DFallback(probing: _arSupported == null);
    }
    return _ArSceneView(
      destinationName: widget.destinationName,
      hasModels: _arModelsPresent,
      bearingDegrees: _bearingToDestination,
      headingDegrees: _heading,
      onRenderError: () {
        _safeSetState(() {
          _arRenderFailed = true;
          _show2DFallback = true;
        });
      },
    );
  }

  /// §4.1 2D fallback: the same LiveMapWidget used across Request/Tracking,
  /// with the rider marker, the pickup pin and a straight walking line.
  Widget _build2DFallback({required bool probing}) {
    final dest = LatLng(widget.destinationLat, widget.destinationLng);
    final me = _currentPosition;
    return Stack(
      children: [
        Positioned.fill(
          child: LiveMapWidget(
            key: _mapKey,
            initialCenter: me ?? dest,
            initialZoom: 17.0,
            interactionMode: MapInteractionMode.full,
            showRecenterFab: true,
            followUserLocation: me != null,
            recenterTarget: me,
            markers: [
              if (me != null)
                MapMarkerModel(
                  id: 'user',
                  position: me,
                  kind: MapMarkerKind.user,
                  label: 'You',
                ),
              MapMarkerModel(
                id: 'pickup',
                position: dest,
                kind: MapMarkerKind.customPin,
                label: widget.destinationName,
              ),
            ],
            polylines: [
              if (me != null)
                MapPolylineModel(id: 'walk', points: [me, dest]),
            ],
            onCameraIdle: (_) {},
          ),
        ),
        if (probing)
          _fallbackBanner('Checking AR support…'),
        if (!probing && _arSupported == false)
          _fallbackBanner(
              'AR is not available on this device — showing map route'),
        if (!probing && _arSupported == true && _arRenderFailed)
          _fallbackBanner('AR view failed — switched to map route'),
      ],
    );
  }

  Widget _fallbackBanner(String message) {
    return Positioned(
      bottom: 120,
      left: 24,
      right: 24,
      child: Center(
        child: DecoratedBox(
          decoration: BoxDecoration(
            color: Colors.black.withOpacity(0.7),
            borderRadius: BorderRadius.circular(8),
          ),
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
            child: Text(
              message,
              textAlign: TextAlign.center,
              style: const TextStyle(color: AppColors.textOnDark, fontSize: 13),
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildHeader() {
    return Positioned(
      top: MediaQuery.of(context).padding.top + 8,
      left: 16,
      right: 16,
      child: Container(
        padding: const EdgeInsets.all(16),
        decoration: BoxDecoration(
          color: Colors.black.withOpacity(0.7),
          borderRadius: BorderRadius.circular(12),
        ),
        child: Row(
          children: [
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                mainAxisSize: MainAxisSize.min,
                children: [
                  Text(
                    widget.destinationName,
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    style: const TextStyle(
                      color: AppColors.textOnDark,
                      fontSize: 18,
                      fontWeight: FontWeight.bold,
                    ),
                  ),
                  const SizedBox(height: 6),
                  Row(
                    children: [
                      const Icon(Icons.directions_walk,
                          color: AppColors.textOnDark, size: 18),
                      const SizedBox(width: 8),
                      Semantics(
                        label: 'Walking distance to pickup: $_distanceLabel',
                        child: Text(
                          _distanceLabel,
                          style: const TextStyle(
                            color: AppColors.textOnDark,
                            fontSize: 16,
                          ),
                        ),
                      ),
                      const Spacer(),
                      _directionChip(),
                    ],
                  ),
                ],
              ),
            ),
            IconButton(
              onPressed: () => Navigator.of(context).pop(),
              // §4.4: white-on-dark camera-overlay chrome; tokenized.
              icon: const Icon(Icons.close, color: AppColors.textOnDark),
              tooltip: 'Close AR navigation',
            ),
          ],
        ),
      ),
    );
  }

  /// Shows the live compass heading when the device provides one; otherwise
  /// the computed bearing to the pickup; otherwise an honest "no compass"
  /// state — never a frozen fake number.
  Widget _directionChip() {
    final heading = _heading;
    final bearing = _bearingToDestination;
    final String text;
    if (heading != null && heading.isFinite) {
      text = 'Heading ${heading.toStringAsFixed(0)}°';
    } else if (bearing.isFinite) {
      text = 'Bearing ${bearing.toStringAsFixed(0)}°';
    } else {
      text = 'Compass unavailable';
    }
    return Text(text, style: const TextStyle(color: Colors.white70));
  }

  Widget _buildControls() {
    return Positioned(
      bottom: MediaQuery.of(context).padding.bottom + 16,
      left: 16,
      right: 16,
      child: SafeArea(
        top: false,
        child: Row(
          children: [
            Expanded(
              child: ElevatedButton.icon(
                onPressed: () => Navigator.of(context).pop(),
                icon: const Icon(Icons.close),
                label: const Text('Exit'),
              ),
            ),
            const SizedBox(width: 12),
            Expanded(
              child: ElevatedButton.icon(
                // §4.1: previously an empty handler; now toggles between the
                // AR scene and the 2D LiveMapWidget fallback. When the probe
                // already reported AR unsupported the button is disabled —
                // tapping it could never produce an AR view.
                onPressed: _arSupported == true
                    ? () =>
                        setState(() => _show2DFallback = !_show2DFallback)
                    : null,
                icon: Icon(_show2DFallback
                    ? Icons.view_ar
                    : Icons.map_outlined),
                label: Text(_show2DFallback ? 'AR View' : 'Map View'),
              ),
            ),
          ],
        ),
      ),
    );
  }

  @override
  void dispose() {
    // The pre-§4.1 version leaked both streams; cancel them here.
    _positionSub?.cancel();
    _headingSub?.cancel();
    super.dispose();
  }
}

/// Owns the ARView lifecycle and reports render errors to the parent so it
/// can drop to the 2D fallback. Construction happens inside an error zone:
/// a platform-side throw discovered during view creation (late ARCore
/// version mismatch, GPU loss) degrades gracefully instead of crashing.
class _ArSceneView extends StatefulWidget {
  final String destinationName;
  final bool hasModels;
  final double bearingDegrees;
  final double? headingDegrees;
  final VoidCallback onRenderError;

  const _ArSceneView({
    required this.destinationName,
    required this.hasModels,
    required this.bearingDegrees,
    required this.headingDegrees,
    required this.onRenderError,
  });

  @override
  State<_ArSceneView> createState() => _ArSceneViewState();
}

class _ArSceneViewState extends State<_ArSceneView> {
  ARSessionManager? _sessionManager;
  ARObjectManager? _objectManager;
  final List<ARNode> _nodes = [];
  bool _creationFailed = false;

  void _onARViewCreated(
    ARSessionManager session,
    ARObjectManager objectManager,
    ARAnchorManager anchorManager,
    ARLocationManager locationManager,
  ) {
    _sessionManager = session;
    _objectManager = objectManager;
    try {
      session.onInitialize(
        showFeaturePoints: false,
        showPlanes: true,
        customPlaneTexturePath: 'textures/plane_texture.png',
        showWorldOrigin: false,
        handlePans: false,
        handleRotation: false,
      );
      objectManager.onInitialize(onImagesTracked: _onImagesTracked);
    } catch (_) {
      widget.onRenderError();
    }
  }

  void _onImagesTracked(List<ARNode> images) {
    // No image anchors are configured; kept as the required callback sink.
  }

  void _syncArrowNodes() {
    final objectManager = _objectManager;
    if (objectManager == null) return;
    if (!widget.hasModels) return; // Bearing overlay handles guidance instead.
    final bearing = widget.bearingDegrees;
    if (!bearing.isFinite) return;
    final heading = widget.headingDegrees;
    final relativeAngle =
        (heading != null && heading.isFinite) ? bearing - heading : bearing;

    // Replace the previous arrow set atomically per update tick.
    for (final node in _nodes) {
      objectManager.removeNode(node);
    }
    _nodes.clear();

    final arrow = ARNode(
      type: NodeType.LOCAL_AND_WEB,
      uri: 'assets/models/arrow.glb',
      scale: vector_math.Vector3(0.5, 0.5, 0.5),
      position: vector_math.Vector3(0.0, 0.0, -2.0),
      rotation: vector_math.Vector4(
          0.0, 1.0, 0.0, relativeAngle * 3.1415926535 / 180.0),
    );
    objectManager.addNode(arrow);
    _nodes.add(arrow);
  }

  @override
  void didUpdateWidget(covariant _ArSceneView oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.bearingDegrees != widget.bearingDegrees ||
        oldWidget.headingDegrees != widget.headingDegrees) {
      _syncArrowNodes();
    }
  }

  @override
  Widget build(BuildContext context) {
    if (_creationFailed) return const SizedBox.shrink();
    try {
      return Stack(
        children: [
          Positioned.fill(
            child: ARView(
              onARViewCreated: _onARViewCreated,
            ),
          ),
          // When the glTF models are not bundled, a plain-Flutter bearing
          // chevron carries the direction information honestly.
          if (!widget.hasModels) _bearingOverlay(),
        ],
      );
    } catch (_) {
      WidgetsBinding.instance
          .addPostFrameCallback((_) => widget.onRenderError());
      _creationFailed = true;
      return const SizedBox.shrink();
    }
  }

  Widget _bearingOverlay() {
    final bearing = widget.bearingDegrees;
    final heading = widget.headingDegrees;
    if (!bearing.isFinite) {
      return const Positioned(
        top: 140,
        left: 0,
        right: 0,
        child: Center(
          child: Text(
            'Waiting for GPS fix…',
            style: TextStyle(color: Colors.white70),
          ),
        ),
      );
    }
    final relative =
        (heading != null && heading.isFinite) ? bearing - heading : bearing;
    return Positioned(
      top: 140,
      left: 0,
      right: 0,
      child: Center(
        child: Transform.rotate(
          angle: relative * 3.1415926535 / 180.0,
          child: const Icon(
            Icons.navigation,
            color: AppColors.primaryLight,
            size: 64,
            semanticLabel: 'Direction to your pickup point',
          ),
        ),
      ),
    );
  }
}
