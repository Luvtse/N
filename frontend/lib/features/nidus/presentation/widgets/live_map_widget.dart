import 'dart:async';
import 'package:flutter/material.dart';
import 'package:google_maps_flutter/google_maps_flutter.dart';

import '../../../../core/theme/app_colors.dart';
import '../../../../core/network/websocket_client.dart';

/// Live map widget showing real-time driver location
class LiveMapWidget extends StatefulWidget {
  final LatLng? pickupLocation;
  final LatLng? dropoffLocation;
  final LatLng? initialDriverLocation;
  final String? rideId;
  final bool showRoute;
  final WebSocketClient? webSocketClient;

  const LiveMapWidget({
    super.key,
    this.pickupLocation,
    this.dropoffLocation,
    this.initialDriverLocation,
    this.rideId,
    this.showRoute = true,
    this.webSocketClient,
  });

  @override
  State<LiveMapWidget> createState() => _LiveMapWidgetState();
}

class _LiveMapWidgetState extends State<LiveMapWidget> {
  GoogleMapController? _mapController;
  final Set<Marker> _markers = {};
  final Set<Polyline> _polylines = {};
  
  LatLng? _driverLocation;
  StreamSubscription<Map<String, dynamic>>? _locationSubscription;
  
  // Bitmap descriptors for markers
  static final BitmapDescriptor _pickupIcon = BitmapDescriptor.defaultMarkerWithHue(
    BitmapDescriptor.hueGreen,
  );
  static final BitmapDescriptor _dropoffIcon = BitmapDescriptor.defaultMarkerWithHue(
    BitmapDescriptor.hueRed,
  );
  static final BitmapDescriptor _driverIcon = BitmapDescriptor.defaultMarkerWithHue(
    BitmapDescriptor.hueAzure,
  );

  @override
  void initState() {
    super.initState();
    _driverLocation = widget.initialDriverLocation;
    _initializeMarkers();
    _subscribeToLocationUpdates();
  }

  @override
  void dispose() {
    _mapController?.dispose();
    _locationSubscription?.cancel();
    super.dispose();
  }

  void _initializeMarkers() {
    // Add pickup marker
    if (widget.pickupLocation != null) {
      _markers.add(
        Marker(
          markerId: const MarkerId('pickup'),
          position: widget.pickupLocation!,
          icon: _pickupIcon,
          infoWindow: const InfoWindow(
            title: 'Pickup',
            snippet: 'Pickup location',
          ),
        ),
      );
    }

    // Add dropoff marker
    if (widget.dropoffLocation != null) {
      _markers.add(
        Marker(
          markerId: const MarkerId('dropoff'),
          position: widget.dropoffLocation!,
          icon: _dropoffIcon,
          infoWindow: const InfoWindow(
            title: 'Dropoff',
            snippet: 'Dropoff location',
          ),
        ),
      );
    }

    // Add driver marker
    if (_driverLocation != null) {
      _markers.add(
        Marker(
          markerId: const MarkerId('driver'),
          position: _driverLocation!,
          icon: _driverIcon,
          infoWindow: const InfoWindow(
            title: 'Your Driver',
            snippet: 'On the way',
          ),
        ),
      );
    }

    // Draw route if both locations are available
    if (widget.showRoute && 
        widget.pickupLocation != null && 
        widget.dropoffLocation != null) {
      _drawRoute();
    }
  }

  void _subscribeToLocationUpdates() {
    if (widget.webSocketClient == null || widget.rideId == null) {
      return;
    }

    _locationSubscription = widget.webSocketClient!.messageStream
        .where((msg) => msg['type'] == 'driver.location')
        .listen((msg) {
      final data = msg['data'] as Map<String, dynamic>?;
      if (data != null) {
        final lat = data['latitude'] as double?;
        final lng = data['longitude'] as double?;
        if (lat != null && lng != null) {
          _updateDriverLocation(LatLng(lat, lng));
        }
      }
    });
  }

  void _updateDriverLocation(LatLng newLocation) {
    setState(() {
      _driverLocation = newLocation;
      
      // Update driver marker
      _markers.removeWhere((m) => m.markerId.value == 'driver');
      _markers.add(
        Marker(
          markerId: const MarkerId('driver'),
          position: newLocation,
          icon: _driverIcon,
          infoWindow: const InfoWindow(
            title: 'Your Driver',
            snippet: 'On the way',
          ),
          anchor: const Offset(0.5, 0.5),
        ),
      );
    });

    // Animate camera to follow driver
    _mapController?.animateCamera(
      CameraUpdate.newLatLng(newLocation),
    );
  }

  void _drawRoute() {
    // In production, use Google Directions API to get actual route
    // For now, draw a simple line between points
    _polylines.add(
      Polyline(
        polylineId: const PolylineId('route'),
        color: AppColors.primary,
        width: 5,
        points: [
          widget.pickupLocation!,
          widget.dropoffLocation!,
        ],
        patterns: [PatternItem.dash(20), PatternItem.gap(10)],
      ),
    );
  }

  void _fitBounds() {
    if (_markers.isEmpty || _mapController == null) return;

    final positions = _markers.map((m) => m.position).toList();
    if (positions.length == 1) {
      _mapController!.animateCamera(
        CameraUpdate.newLatLngZoom(positions.first, 15),
      );
      return;
    }

    double minLat = positions.first.latitude;
    double maxLat = positions.first.latitude;
    double minLng = positions.first.longitude;
    double maxLng = positions.first.longitude;

    for (final pos in positions) {
      if (pos.latitude < minLat) minLat = pos.latitude;
      if (pos.latitude > maxLat) maxLat = pos.latitude;
      if (pos.longitude < minLng) minLng = pos.longitude;
      if (pos.longitude > maxLng) maxLng = pos.longitude;
    }

    final bounds = LatLngBounds(
      southwest: LatLng(minLat, minLng),
      northeast: LatLng(maxLat, maxLng),
    );

    _mapController!.animateCamera(
      CameraUpdate.newLatLngBounds(bounds, 50),
    );
  }

  @override
  Widget build(BuildContext context) {
    // Determine initial camera position
    LatLng initialPosition = const LatLng(40.7128, -74.0060); // Default NYC
    if (widget.pickupLocation != null) {
      initialPosition = widget.pickupLocation!;
    } else if (_driverLocation != null) {
      initialPosition = _driverLocation!;
    }

    return Stack(
      children: [
        GoogleMap(
          initialCameraPosition: CameraPosition(
            target: initialPosition,
            zoom: 14,
          ),
          onMapCreated: (controller) {
            _mapController = controller;
            // Fit bounds after map is created
            Future.delayed(const Duration(milliseconds: 500), _fitBounds);
          },
          markers: _markers,
          polylines: _polylines,
          myLocationEnabled: true,
          myLocationButtonEnabled: true,
          zoomControlsEnabled: true,
          compassEnabled: true,
          mapToolbarEnabled: false,
          trafficEnabled: true,
          buildingsEnabled: true,
          padding: const EdgeInsets.all(16),
        ),

        // Legend overlay
        Positioned(
          top: 16,
          right: 16,
          child: Container(
            padding: const EdgeInsets.all(12),
            decoration: BoxDecoration(
              color: Colors.white,
              borderRadius: BorderRadius.circular(12),
              boxShadow: [
                BoxShadow(
                  color: Colors.black.withOpacity(0.1),
                  blurRadius: 8,
                ),
              ],
            ),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisSize: MainAxisSize.min,
              children: [
                _buildLegendItem(_pickupIcon, 'Pickup'),
                const SizedBox(height: 8),
                _buildLegendItem(_dropoffIcon, 'Dropoff'),
                if (_driverLocation != null) ...[
                  const SizedBox(height: 8),
                  _buildLegendItem(_driverIcon, 'Driver'),
                ],
              ],
            ),
          ),
        ),

        // Recenter button
        Positioned(
          bottom: 16,
          right: 16,
          child: FloatingActionButton(
            heroTag: 'recenter',
            mini: true,
            onPressed: _fitBounds,
            backgroundColor: Colors.white,
            child: const Icon(Icons.my_location, color: AppColors.primary),
          ),
        ),
      ],
    );
  }

  Widget _buildLegendItem(BitmapDescriptor icon, String label) {
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Icon(
          Icons.location_on,
          color: icon == _pickupIcon
              ? Colors.green
              : icon == _dropoffIcon
                  ? Colors.red
                  : Colors.blue,
          size: 16,
        ),
        const SizedBox(width: 4),
        Text(
          label,
          style: const TextStyle(fontSize: 12),
        ),
      ],
    );
  }
}