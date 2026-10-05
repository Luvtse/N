import 'package:flutter/material.dart';
import 'package:ar_flutter_plugin/ar_flutter_plugin.dart';
import 'package:ar_flutter_plugin/datatypes/node_types.dart';
import 'package:ar_flutter_plugin/managers/ar_anchor_manager.dart';
import 'package:ar_flutter_plugin/managers/ar_location_manager.dart';
import 'package:ar_flutter_plugin/managers/ar_object_manager.dart';
import 'package:ar_flutter_plugin/managers/ar_session_manager.dart';
import 'package:ar_flutter_plugin/models/ar_node.dart';
import 'package:vector_math/vector_math_64.dart' as VectorMath;
import 'package:geolocator/geolocator.dart';

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
  late ARSessionManager arSessionManager;
  late ARObjectManager arObjectManager;
  late ARAnchorManager arAnchorManager;
  late ARLocationManager arLocationManager;
  
  List<ARNode> nodes = [];
  Position? currentPosition;
  double heading = 0;
  double distanceToDestination = 0;
  
  @override
  void initState() {
    super.initState();
    _startLocationUpdates();
  }
  
  Future<void> _startLocationUpdates() async {
    // Get continuous location updates
    Geolocator.getPositionStream(
      locationSettings: const LocationSettings(
        accuracy: LocationAccuracy.high,
        distanceFilter: 5,
      ),
    ).listen((position) {
      setState(() {
        currentPosition = position;
        _updateNavigation();
      });
    });
    
    // Get compass heading
    Geolocator.getHeadingStream().listen((heading) {
      setState(() {
        this.heading = heading;
      });
    });
  }
  
  void _updateNavigation() {
    if (currentPosition == null) return;
    
    // Calculate distance and bearing to destination
    distanceToDestination = Geolocator.distanceBetween(
      currentPosition!.latitude,
      currentPosition!.longitude,
      widget.destinationLat,
      widget.destinationLng,
    );
    
    final bearing = Geolocator.bearingBetween(
      currentPosition!.latitude,
      currentPosition!.longitude,
      widget.destinationLat,
      widget.destinationLng,
    );
    
    // Update AR arrows
    _updateARArrows(bearing);
  }
  
  void _updateARArrows(double bearing) {
    // Remove old nodes
    for (var node in nodes) {
      arObjectManager.removeNode(node);
    }
    nodes.clear();
    
    // Calculate relative angle
    final relativeAngle = bearing - heading;
    
    // Create directional arrow
    final arrowNode = ARNode(
      type: NodeType.LOCAL_AND_WEB,
      uri: "assets/models/arrow.glb",
      scale: VectorMath.Vector3(0.5, 0.5, 0.5),
      position: VectorMath.Vector3(
        0.0,
        0.0,
        -2.0, // 2 meters in front
      ),
      rotation: VectorMath.Vector4(
        0.0,
        1.0,
        0.0,
        relativeAngle * 3.14159 / 180,
      ),
    );
    
    arObjectManager.addNode(arrowNode);
    nodes.add(arrowNode);
    
    // Add distance markers every 10 meters
    if (distanceToDestination > 10) {
      final markerCount = (distanceToDestination / 10).floor();
      for (int i = 1; i <= markerCount && i <= 5; i++) {
        final markerNode = ARNode(
          type: NodeType.LOCAL_AND_WEB,
          uri: "assets/models/distance_marker.glb",
          scale: VectorMath.Vector3(0.3, 0.3, 0.3),
          position: VectorMath.Vector3(
            0.0,
            0.0,
            -2.0 * i,
          ),
        );
        arObjectManager.addNode(markerNode);
        nodes.add(markerNode);
      }
    }
  }
  
  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: Stack(
        children: [
          // AR View
          ARView(
            onARViewCreated: (session, objectManager, anchorManager, locationManager) {
              arSessionManager = session;
              arObjectManager = objectManager;
              arAnchorManager = anchorManager;
              arLocationManager = locationManager;
              
              arSessionManager.onInitialize(
                showFeaturePoints: false,
                showPlanes: true,
                customPlaneTexturePath: "assets/textures/plane_texture.png",
                showWorldOrigin: false,
              );
              
              arObjectManager.onInitialize(showPlanes: true);
            },
          ),
          
          // Navigation overlay
          Positioned(
            top: 50,
            left: 20,
            right: 20,
            child: Container(
              padding: const EdgeInsets.all(16),
              decoration: BoxDecoration(
                color: Colors.black.withOpacity(0.7),
                borderRadius: BorderRadius.circular(12),
              ),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    widget.destinationName,
                    style: const TextStyle(
                      color: Colors.white,
                      fontSize: 20,
                      fontWeight: FontWeight.bold,
                    ),
                  ),
                  const SizedBox(height: 8),
                  Row(
                    children: [
                      const Icon(Icons.directions_walk, color: Colors.white),
                      const SizedBox(width: 8),
                      Text(
                        '${distanceToDestination.toStringAsFixed(0)} m',
                        style: const TextStyle(
                          color: Colors.white,
                          fontSize: 18,
                        ),
                      ),
                      const Spacer(),
                      Text(
                        '${heading.toStringAsFixed(0)}°',
                        style: const TextStyle(color: Colors.white70),
                      ),
                    ],
                  ),
                ],
              ),
            ),
          ),
          
          // Turn指示
          if (distanceToDestination < 20)
            Positioned(
              top: MediaQuery.of(context).size.height / 2 - 50,
              left: MediaQuery.of(context).size.width / 2 - 50,
              child: Container(
                width: 100,
                height: 100,
                decoration: BoxDecoration(
                  color: Colors.blue.withOpacity(0.8),
                  shape: BoxShape.circle,
                ),
                child: const Icon(
                  Icons.arrow_upward,
                  color: Colors.white,
                  size: 60,
                ),
              ),
            ),
          
          // Controls
          Positioned(
            bottom: 50,
            left: 20,
            right: 20,
            child: Row(
              children: [
                Expanded(
                  child: ElevatedButton.icon(
                    onPressed: () => Navigator.pop(context),
                    icon: const Icon(Icons.close),
                    label: const Text('Exit AR'),
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: ElevatedButton.icon(
                    onPressed: () {
                      // Switch to 2D map
                    },
                    icon: const Icon(Icons.map),
                    label: const Text('Map View'),
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
  
  @override
  void dispose() {
    for (var node in nodes) {
      arObjectManager.removeNode(node);
    }
    arSessionManager.dispose();
    super.dispose();
  }
}