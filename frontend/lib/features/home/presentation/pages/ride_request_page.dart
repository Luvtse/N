import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:go_router/go_router.dart';
import '../../../../core/theme/app_colors.dart';
import '../bloc/ride_bloc.dart';
import '../widgets/map_widget.dart';

class RideRequestPage extends StatefulWidget {
  const RideRequestPage({super.key});

  @override
  State<RideRequestPage> createState() => _RideRequestPageState();
}

class _RideRequestPageState extends State<RideRequestPage> {
  final _pickupController = TextEditingController();
  final _dropoffController = TextEditingController();

  @override
  void dispose() {
    _pickupController.dispose();
    _dropoffController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Request a Ride'),
        leading: IconButton(
          icon: const Icon(Icons.arrow_back),
          onPressed: () => context.pop(),
        ),
      ),
      body: BlocConsumer<RideBloc, RideState>(
        listener: (context, state) {
          if (state is RideRequested) {
            context.push('/nidus/tracking/${state.rideId}');
          } else if (state is RideError) {
            ScaffoldMessenger.of(context).showSnackBar(
              SnackBar(
                content: Text(state.message),
                backgroundColor: AppColors.error,
              ),
            );
          }
        },
        builder: (context, state) {
          return Column(
            children: [
              // Map
              Expanded(
                flex: 2,
                child: MapWidget(
                  showUserLocation: true,
                  onMapCreated: () {},
                ),
              ),
              
              // Bottom Sheet
              Expanded(
                flex: 3,
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
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      const Text(
                        'Where to?',
                        style: TextStyle(
                          fontSize: 24,
                          fontWeight: FontWeight.bold,
                        ),
                      ),
                      const SizedBox(height: 24),
                      
                      // Pickup Location
                      TextField(
                        controller: _pickupController,
                        decoration: InputDecoration(
                          hintText: 'Pickup location',
                          prefixIcon: Container(
                            margin: const EdgeInsets.all(8),
                            width: 24,
                            height: 24,
                            decoration: const BoxDecoration(
                              color: AppColors.success,
                              shape: BoxShape.circle,
                            ),
                          ),
                        ),
                      ),
                      
                      const SizedBox(height: 16),
                      
                      // Dropoff Location
                      TextField(
                        controller: _dropoffController,
                        decoration: InputDecoration(
                          hintText: 'Dropoff location',
                          prefixIcon: Container(
                            margin: const EdgeInsets.all(8),
                            width: 24,
                            height: 24,
                            decoration: const BoxDecoration(
                              color: AppColors.error,
                              shape: BoxShape.circle,
                            ),
                          ),
                        ),
                      ),
                      
                      const SizedBox(height: 24),
                      
                      // Ride Options
                      const Text(
                        'Choose ride type',
                        style: TextStyle(
                          fontSize: 16,
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                      const SizedBox(height: 12),
                      
                      Row(
                        children: [
                          _buildRideOption(
                            icon: Icons.directions_car,
                            title: 'Standard',
                            price: '\$12-15',
                            isSelected: true,
                          ),
                          const SizedBox(width: 12),
                          _buildRideOption(
                            icon: Icons.star,
                            title: 'Premium',
                            price: '\$20-25',
                          ),
                          const SizedBox(width: 12),
                          _buildRideOption(
                            icon: Icons.electric_car,
                            title: 'Electric',
                            price: '\$14-18',
                          ),
                        ],
                      ),
                      
                      const Spacer(),
                      
                      // Request Button
                      SizedBox(
                        width: double.infinity,
                        child: ElevatedButton(
                          onPressed: state is RideLoading
                              ? null
                              : () {
                                  context.read<RideBloc>().add(
                                        RequestRide(
                                          40.7128,
                                          -74.0060,
                                          40.7589,
                                          -73.9851,
                                        ),
                                      );
                                },
                          child: state is RideLoading
                              ? const SizedBox(
                                  width: 20,
                                  height: 20,
                                  child: CircularProgressIndicator(
                                    strokeWidth: 2,
                                    valueColor: AlwaysStoppedAnimation<Color>(
                                      Colors.white,
                                    ),
                                  ),
                                )
                              : const Text('Request Ride'),
                        ),
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

  Widget _buildRideOption({
    required IconData icon,
    required String title,
    required String price,
    bool isSelected = false,
  }) {
    return Expanded(
      child: Container(
        padding: const EdgeInsets.all(16),
        decoration: BoxDecoration(
          color: isSelected ? AppColors.primary.withOpacity(0.1) : AppColors.inputBackground,
          borderRadius: BorderRadius.circular(12),
          border: Border.all(
            color: isSelected ? AppColors.primary : Colors.transparent,
            width: 2,
          ),
        ),
        child: Column(
          children: [
            Icon(
              icon,
              color: isSelected ? AppColors.primary : AppColors.textSecondary,
              size: 32,
            ),
            const SizedBox(height: 8),
            Text(
              title,
              style: TextStyle(
                fontWeight: FontWeight.w600,
                color: isSelected ? AppColors.primary : AppColors.textPrimary,
              ),
            ),
            const SizedBox(height: 4),
            Text(
              price,
              style: TextStyle(
                fontSize: 12,
                color: AppColors.textSecondary,
              ),
            ),
          ],
        ),
      ),
    );
  }
}