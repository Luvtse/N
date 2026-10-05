import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:url_launcher/url_launcher.dart';
import '../../../../core/theme/app_colors.dart';
import '../bloc/active_ride_bloc.dart';
import '../widgets/rider_info_card.dart';
import '../widgets/navigation_controls.dart';

class RideNavigationPage extends StatelessWidget {
  final String rideId;

  const RideNavigationPage({super.key, required this.rideId});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: BlocBuilder<ActiveRideBloc, ActiveRideState>(
        builder: (context, state) {
          if (state is! ActiveRideLoaded) {
            return const Center(child: CircularProgressIndicator());
          }

          return Stack(
            children: [
              // Map
              Container(
                color: Colors.grey[300],
                child: const Center(
                  child: Text('Map View\n(Integrate Google Maps)'),
                ),
              ),

              // Top Bar
              Positioned(
                top: 50,
                left: 16,
                right: 16,
                child: Row(
                  children: [
                    Container(
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
                      child: IconButton(
                        icon: const Icon(Icons.arrow_back),
                        onPressed: () => Navigator.pop(context),
                      ),
                    ),
                    const Spacer(),
                    Container(
                      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
                      decoration: BoxDecoration(
                        color: AppColors.primary,
                        borderRadius: BorderRadius.circular(12),
                      ),
                      child: Text(
                        '\$${state.ride.fare.toStringAsFixed(2)}',
                        style: const TextStyle(
                          color: Colors.white,
                          fontWeight: FontWeight.bold,
                          fontSize: 18,
                        ),
                      ),
                    ),
                  ],
                ),
              ),

              // Bottom Sheet
              Positioned(
                bottom: 0,
                left: 0,
                right: 0,
                child: Container(
                  padding: const EdgeInsets.all(24),
                  decoration: const BoxDecoration(
                    color: Colors.white,
                    borderRadius: BorderRadius.only(
                      topLeft: Radius.circular(24),
                      topRight: Radius.circular(24),
                    ),
                  ),
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      // Status
                      _buildStatusIndicator(state.ride.status),

                      const SizedBox(height: 20),

                      // Rider Info
                      RiderInfoCard(
                        riderName: state.ride.riderName,
                        riderRating: state.ride.riderRating,
                        pickupAddress: state.ride.pickupAddress,
                        dropoffAddress: state.ride.dropoffAddress,
                      ),

                      const SizedBox(height: 20),

                      // Navigation Controls
                      NavigationControls(
                        status: state.ride.status,
                        onStartNavigation: () {
                          _openNavigation(
                            state.ride.pickupLat,
                            state.ride.pickupLng,
                          );
                        },
                        onStartRide: () {
                          context.read<ActiveRideBloc>().add(StartRide(rideId));
                        },
                        onCompleteRide: () {
                          context.read<ActiveRideBloc>().add(CompleteRide(rideId));
                        },
                      ),

                      const SizedBox(height: 16),

                      // Action Buttons
                      Row(
                        children: [
                          Expanded(
                            child: OutlinedButton.icon(
                              onPressed: () {
                                // Call rider
                              },
                              icon: const Icon(Icons.call),
                              label: const Text('Call'),
                            ),
                          ),
                          const SizedBox(width: 12),
                          Expanded(
                            child: ElevatedButton.icon(
                              onPressed: () {
                                // Message rider
                              },
                              icon: const Icon(Icons.message),
                              label: const Text('Message'),
                            ),
                          ),
                        ],
                      ),
                    ],
                  ),
                ),
              ),
            ],
          );
        },
      ),
    );
  }

  Widget _buildStatusIndicator(String status) {
    String text;
    Color color;
    IconData icon;

    switch (status) {
      case 'driver_en_route':
        text = 'En route to pickup';
        color = AppColors.info;
        icon = Icons.directions_car;
        break;
      case 'in_progress':
        text = 'Ride in progress';
        color = AppColors.success;
        icon = Icons.navigation;
        break;
      default:
        text = 'Preparing';
        color = Colors.grey;
        icon = Icons.hourglass_empty;
    }

    return Row(
      children: [
        Icon(icon, color: color, size: 24),
        const SizedBox(width: 12),
        Text(
          text,
          style: TextStyle(
            fontSize: 18,
            fontWeight: FontWeight.w600,
            color: color,
          ),
        ),
      ],
    );
  }

  Future<void> _openNavigation(double lat, double lng) async {
    final url = 'google.navigation:q=$lat,$lng';
    if (await canLaunchUrl(Uri.parse(url))) {
      await launchUrl(Uri.parse(url));
    }
  }
}