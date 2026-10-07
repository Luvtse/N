import 'package:flutter/material.dart';

/// Placeholder map widget used by ride request/tracking pages.
///
/// The real map integration (Google Maps / Mapbox) is intentionally not wired
/// in yet; this stub keeps the app compiling and renders a static preview.
class MapWidget extends StatelessWidget {
  final bool showUserLocation;
  final bool showDriverLocation;
  final VoidCallback? onMapCreated;

  const MapWidget({
    super.key,
    this.showUserLocation = false,
    this.showDriverLocation = false,
    this.onMapCreated,
  });

  @override
  Widget build(BuildContext context) {
    // Fire the creation callback once the placeholder is laid out.
    WidgetsBinding.instance.addPostFrameCallback((_) {
      onMapCreated?.call();
    });

    return Container(
      color: Colors.grey[300],
      child: Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              Icons.map_outlined,
              size: 48,
              color: Colors.grey[600],
            ),
            const SizedBox(height: 8),
            Text(
              'Map View\n(Integrate Google Maps)',
              textAlign: TextAlign.center,
              style: TextStyle(color: Colors.grey[700]),
            ),
            if (showUserLocation)
              const Padding(
                padding: EdgeInsets.only(top: 8.0),
                child: Icon(Icons.person_pin_circle, color: Colors.blue),
              ),
            if (showDriverLocation)
              const Padding(
                padding: EdgeInsets.only(top: 4.0),
                child: Icon(Icons.directions_car, color: Colors.green),
              ),
          ],
        ),
      ),
    );
  }
}
