import 'package:flutter/material.dart';
import 'package:google_maps_flutter/google_maps_flutter.dart';
import 'package:geolocator/geolocator.dart';

import '../../../../core/theme/app_colors.dart';

class DriverMap extends StatefulWidget {
  final Position? currentPosition;
  final bool isOnline;

  const DriverMap({
    super.key,
    this.currentPosition,
    required this.isOnline,
  });

  @override
  State<DriverMap> createState() => _DriverMapState();
}

class _DriverMapState extends State<DriverMap> {
  GoogleMapController? _mapController;
  final Set<Marker> _markers = {};

  @override
  void dispose() {
    _mapController?.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final defaultPosition = widget.currentPosition != null
        ? LatLng(
            widget.currentPosition!.latitude,
            widget.currentPosition!.longitude,
          )
        : const LatLng(40.7128, -74.0060); // Default to NYC

    return GoogleMap(
      initialCameraPosition: CameraPosition(
        target: defaultPosition,
        zoom: 14,
      ),
      onMapCreated: (controller) {
        _mapController = controller;
        _updateDriverMarker(defaultPosition);
      },
      markers: _markers,
      myLocationEnabled: widget.isOnline,
      myLocationButtonEnabled: true,
      zoomControlsEnabled: false,
      compassEnabled: true,
      mapToolbarEnabled: false,
      onCameraMove: () {},
    );
  }

  void _updateDriverMarker(LatLng position) {
    setState(() {
      _markers.add(
        Marker(
          markerId: const MarkerId('driver_location'),
          position: position,
          icon: BitmapDescriptor.defaultMarkerWithHue(
            BitmapDescriptor.hueAzure,
          ),
          infoWindow: const InfoWindow(
            title: 'Your Location',
            snippet: 'You are here',
          ),
        ),
      );
    });
  }

  void _moveToPosition(LatLng position) {
    _mapController?.animateCamera(
      CameraUpdate.newLatLng(position),
    );
  }
}