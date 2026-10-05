import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:go_router/go_router.dart';

import '../../../../core/theme/app_colors.dart';
import '../bloc/ride_bloc.dart';
import '../widgets/ride_type_selector.dart';
import '../widgets/location_input.dart';

class RideRequestPage extends StatefulWidget {
  const RideRequestPage({super.key});

  @override
  State<RideRequestPage> createState() => _RideRequestPageState();
}

class _RideRequestPageState extends State<RideRequestPage> {
  final _pickupController = TextEditingController();
  final _dropoffController = TextEditingController();
  
  double _pickupLat = 40.7128;
  double _pickupLng = -74.0060;
  double _dropoffLat = 40.7589;
  double _dropoffLng = -73.9851;
  
  String _selectedRideType = 'standard';

  @override
  void dispose() {
    _pickupController.dispose();
    _dropoffController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: AppColors.background,
      appBar: AppBar(
        title: const Text('Request a Ride'),
        backgroundColor: Colors.white,
        elevation: 0,
        leading: IconButton(
          icon: const Icon(Icons.arrow_back, color: AppColors.textPrimary),
          onPressed: () => context.pop(),
        ),
      ),
      body: BlocConsumer<RideBloc, RideState>(
        listener: _handleRideState,
        builder: (context, state) {
          return Column(
            children: [
              // Map placeholder
              _buildMapSection(),

              // Bottom sheet with inputs
              Expanded(
                child: Container(
                  decoration: const BoxDecoration(
                    color: Colors.white,
                    borderRadius: BorderRadius.only(
                      topLeft: Radius.circular(24),
                      topRight: Radius.circular(24),
                    ),
                    boxShadow: [
                      BoxShadow(
                        color: Colors.black12,
                        blurRadius: 20,
                        offset: Offset(0, -4),
                      ),
                    ],
                  ),
                  child: SingleChildScrollView(
                    padding: const EdgeInsets.all(24),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: [
                        // Title
                        const Text(
                          'Where to?',
                          style: TextStyle(
                            fontSize: 24,
                            fontWeight: FontWeight.bold,
                            color: AppColors.textPrimary,
                          ),
                        ),
                        const SizedBox(height: 24),

                        // Pickup location
                        LocationInput(
                          controller: _pickupController,
                          label: 'Pickup location',
                          icon: Icons.circle,
                          iconColor: AppColors.success,
                          onChanged: (value) {
                            // TODO: Geocode address to coordinates
                          },
                        ),

                        const SizedBox(height: 12),

                        // Connector line
                        Container(
                          margin: const EdgeInsets.only(left: 28),
                          height: 20,
                          width: 2,
                          color: AppColors.border,
                        ),

                        const SizedBox(height: 12),

                        // Dropoff location
                        LocationInput(
                          controller: _dropoffController,
                          label: 'Dropoff location',
                          icon: Icons.circle,
                          iconColor: AppColors.error,
                          onChanged: (value) {
                            // TODO: Geocode address to coordinates
                          },
                        ),

                        const SizedBox(height: 32),

                        // Ride type selector
                        const Text(
                          'Choose ride type',
                          style: TextStyle(
                            fontSize: 16,
                            fontWeight: FontWeight.w600,
                            color: AppColors.textPrimary,
                          ),
                        ),
                        const SizedBox(height: 16),

                        RideTypeSelector(
                          selectedType: _selectedRideType,
                          onTypeSelected: (type) {
                            setState(() => _selectedRideType = type);
                            // Get fare estimate for selected type
                            context.read<RideBloc>().add(
                                  GetFareEstimate(
                                    pickupLat: _pickupLat,
                                    pickupLng: _pickupLng,
                                    dropoffLat: _dropoffLat,
                                    dropoffLng: _dropoffLng,
                                  ),
                                );
                          },
                        ),

                        const SizedBox(height: 24),

                        // Fare estimate
                        if (state is FareEstimateLoaded)
                          _buildFareEstimate(state.estimate),

                        const SizedBox(height: 24),

                        // Request button
                        _buildRequestButton(state),

                        if (state is RideLoading) ...[
                          const SizedBox(height: 16),
                          const Center(
                            child: CircularProgressIndicator(
                              color: AppColors.primary,
                            ),
                          ),
                        ],

                        if (state is RideError) ...[
                          const SizedBox(height: 16),
                          _buildErrorMessage(state.message),
                        ],
                      ],
                    ),
                  ),
                ),
              ),
            ],
          );
        },
      ),
    );
  }

  Widget _buildMapSection() {
    return Container(
      height: 280,
      decoration: BoxDecoration(
        color: Colors.grey[200],
        image: const DecorationImage(
          image: AssetImage('assets/images/map_placeholder.png'),
          fit: BoxFit.cover,
        ),
      ),
      child: Stack(
        children: [
          // Map placeholder
          const Center(
            child: Icon(
              Icons.map,
              size: 64,
              color: Colors.grey,
            ),
          ),

          // Pickup marker
          Positioned(
            top: 100,
            left: 120,
            child: _buildMapMarker(
              icon: Icons.circle,
              color: AppColors.success,
              label: 'Pickup',
            ),
          ),

          // Dropoff marker
          Positioned(
            bottom: 80,
            right: 100,
            child: _buildMapMarker(
              icon: Icons.circle,
              color: AppColors.error,
              label: 'Dropoff',
            ),
          ),

          // Route line (simplified)
          CustomPaint(
            size: const Size(200, 150),
            painter: RoutePainter(),
          ),
        ],
      ),
    );
  }

  Widget _buildMapMarker({
    required IconData icon,
    required Color color,
    required String label,
  }) {
    return Column(
      children: [
        Container(
          padding: const EdgeInsets.all(8),
          decoration: BoxDecoration(
            color: color,
            shape: BoxShape.circle,
            boxShadow: [
              BoxShadow(
                color: color.withOpacity(0.4),
                blurRadius: 8,
                spreadRadius: 2,
              ),
            ],
          ),
          child: Icon(icon, color: Colors.white, size: 16),
        ),
        const SizedBox(height: 4),
        Container(
          padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
          decoration: BoxDecoration(
            color: Colors.white,
            borderRadius: BorderRadius.circular(8),
            boxShadow: [
              BoxShadow(
                color: Colors.black.withOpacity(0.1),
                blurRadius: 4,
              ),
            ],
          ),
          child: Text(
            label,
            style: const TextStyle(
              fontSize: 10,
              fontWeight: FontWeight.w600,
              color: AppColors.textPrimary,
            ),
          ),
        ),
      ],
    );
  }

  Widget _buildFareEstimate(dynamic estimate) {
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: AppColors.primary.withOpacity(0.05),
        borderRadius: BorderRadius.circular(16),
        border: Border.all(
          color: AppColors.primary.withOpacity(0.2),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            mainAxisAlignment: MainAxisAlignment.spaceBetween,
            children: [
              const Text(
                'Estimated Fare',
                style: TextStyle(
                  fontSize: 14,
                  color: AppColors.textSecondary,
                ),
              ),
              Text(
                '\$${estimate.totalFare.toStringAsFixed(2)}',
                style: const TextStyle(
                  fontSize: 24,
                  fontWeight: FontWeight.bold,
                  color: AppColors.primary,
                ),
              ),
            ],
          ),
          const SizedBox(height: 8),
          Row(
            children: [
              Icon(
                Icons.directions_car,
                size: 16,
                color: AppColors.textSecondary,
              ),
              const SizedBox(width: 4),
              Text(
                '${estimate.distanceKm.toStringAsFixed(1)} km',
                style: TextStyle(
                  fontSize: 12,
                  color: AppColors.textSecondary,
                ),
              ),
              const SizedBox(width: 16),
              Icon(
                Icons.access_time,
                size: 16,
                color: AppColors.textSecondary,
              ),
              const SizedBox(width: 4),
              Text(
                '${estimate.durationMinutes} min',
                style: TextStyle(
                  fontSize: 12,
                  color: AppColors.textSecondary,
                ),
              ),
              if (estimate.surgeMultiplier > 1.0) ...[
                const SizedBox(width: 16),
                Icon(
                  Icons.trending_up,
                  size: 16,
                  color: AppColors.warning,
                ),
                const SizedBox(width: 4),
                Text(
                  '${estimate.surgeMultiplier}x surge',
                  style: const TextStyle(
                    fontSize: 12,
                    color: AppColors.warning,
                    fontWeight: FontWeight.w600,
                  ),
                ),
              ],
            ],
          ),
        ],
      ),
    );
  }

  Widget _buildRequestButton(RideState state) {
    final isLoading = state is RideLoading;
    final hasLocations = _pickupController.text.isNotEmpty && 
                         _dropoffController.text.isNotEmpty;

    return SizedBox(
      height: 56,
      child: ElevatedButton(
        onPressed: isLoading || !hasLocations ? null : _requestRide,
        style: ElevatedButton.styleFrom(
          backgroundColor: AppColors.primary,
          foregroundColor: Colors.white,
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(16),
          ),
          elevation: 0,
        ),
        child: isLoading
            ? const SizedBox(
                width: 24,
                height: 24,
                child: CircularProgressIndicator(
                  strokeWidth: 2.5,
                  valueColor: AlwaysStoppedAnimation<Color>(Colors.white),
                ),
              )
            : const Row(
                mainAxisAlignment: MainAxisAlignment.center,
                children: [
                  Icon(Icons.directions_car, size: 20),
                  SizedBox(width: 8),
                  Text(
                    'Request Ride',
                    style: TextStyle(
                      fontSize: 16,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                ],
              ),
      ),
    );
  }

  Widget _buildErrorMessage(String message) {
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: AppColors.error.withOpacity(0.1),
        borderRadius: BorderRadius.circular(12),
        border: Border.all(
          color: AppColors.error.withOpacity(0.3),
        ),
      ),
      child: Row(
        children: [
          const Icon(Icons.error_outline, color: AppColors.error, size: 20),
          const SizedBox(width: 12),
          Expanded(
            child: Text(
              message,
              style: const TextStyle(
                color: AppColors.error,
                fontSize: 14,
              ),
            ),
          ),
        ],
      ),
    );
  }

  void _requestRide() {
    context.read<RideBloc>().add(
          RequestRide(
            pickupLat: _pickupLat,
            pickupLng: _pickupLng,
            dropoffLat: _dropoffLat,
            dropoffLng: _dropoffLng,
            pickupAddress: _pickupController.text,
            dropoffAddress: _dropoffController.text,
            rideType: _selectedRideType,
          ),
        );
  }

  void _handleRideState(BuildContext context, RideState state) {
    if (state is RideRequested) {
      context.go('/nidus/tracking/${state.rideId}');
    }
  }
}

/// Custom painter for route line
class RoutePainter extends CustomPainter {
  @override
  void paint(Canvas canvas, Size size) {
    final paint = Paint()
      ..color = AppColors.primary.withOpacity(0.5)
      ..strokeWidth = 3
      ..style = PaintingStyle.stroke;

    final path = Path();
    path.moveTo(0, size.height);
    path.quadraticBezierTo(
      size.width / 2,
      size.height / 2,
      size.width,
      0,
    );

    canvas.drawPath(path, paint);
  }

  @override
  bool shouldRepaint(CustomPainter oldDelegate) => false;
}