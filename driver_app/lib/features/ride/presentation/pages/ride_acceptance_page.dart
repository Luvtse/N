import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:go_router/go_router.dart';

import '../../../../core/theme/app_colors.dart';
import '../../domain/entities/active_ride.dart';
import '../bloc/active_ride_bloc.dart';

class RideAcceptancePage extends StatefulWidget {
  final String rideId;

  const RideAcceptancePage({
    super.key,
    required this.rideId,
  });

  @override
  State<RideAcceptancePage> createState() => _RideAcceptancePageState();
}

class _RideAcceptancePageState extends State<RideAcceptancePage> {
  int _timeLeft = 30;
  Timer? _timer;

  @override
  void initState() {
    super.initState();
    _startTimer();
  }

  void _startTimer() {
    _timer = Timer.periodic(const Duration(seconds: 1), (timer) {
      if (_timeLeft > 0) {
        setState(() => _timeLeft--);
      } else {
        timer.cancel();
        _autoDecline();
      }
    });
  }

  void _autoDecline() {
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(
        content: Text('Ride request expired'),
        backgroundColor: AppColors.warning,
      ),
    );
    context.pop();
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    // TODO: Load actual ride data from repository
    final ride = ActiveRide(
      id: widget.rideId,
      riderId: 'rider_123',
      riderName: 'John Doe',
      riderPhone: '+1234567890',
      riderRating: 4.8,
      pickupLat: 40.7128,
      pickupLng: -74.0060,
      pickupAddress: '123 Broadway, New York, NY',
      dropoffLat: 40.7589,
      dropoffLng: -73.9851,
      dropoffAddress: '456 5th Ave, New York, NY',
      distanceKm: 5.2,
      estimatedDurationMinutes: 18,
      fareAmount: 15.50,
      tipAmount: 0,
      status: 'pending',
      requestedAt: DateTime.now(),
    );

    return Scaffold(
      backgroundColor: Colors.black87,
      body: SafeArea(
        child: Column(
          children: [
            // Timer
            Padding(
              padding: const EdgeInsets.all(24),
              child: Stack(
                alignment: Alignment.center,
                children: [
                  SizedBox(
                    width: 100,
                    height: 100,
                    child: CircularProgressIndicator(
                      value: _timeLeft / 30,
                      strokeWidth: 8,
                      backgroundColor: Colors.white24,
                      valueColor: const AlwaysStoppedAnimation<Color>(
                        AppColors.primary,
                      ),
                    ),
                  ),
                  Text(
                    '$_timeLeft',
                    style: const TextStyle(
                      fontSize: 36,
                      fontWeight: FontWeight.bold,
                      color: Colors.white,
                    ),
                  ),
                ],
              ),
            ),
            
            Expanded(
              child: Container(
                margin: const EdgeInsets.symmetric(horizontal: 24),
                padding: const EdgeInsets.all(24),
                decoration: BoxDecoration(
                  color: Colors.white,
                  borderRadius: BorderRadius.circular(24),
                ),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    // Fare
                    Center(
                      child: Text(
                        '\$${ride.fareAmount.toStringAsFixed(2)}',
                        style: const TextStyle(
                          fontSize: 48,
                          fontWeight: FontWeight.bold,
                          color: AppColors.primary,
                        ),
                      ),
                    ),
                    const SizedBox(height: 8),
                    Center(
                      child: Text(
                        '${ride.distanceKm.toStringAsFixed(1)} km • ${ride.estimatedDurationMinutes} min',
                        style: TextStyle(
                          fontSize: 16,
                          color: Colors.grey[600],
                        ),
                      ),
                    ),
                    const SizedBox(height: 32),
                    
                    // Pickup
                    _buildLocationRow(
                      icon: Icons.circle,
                      iconColor: AppColors.success,
                      label: 'Pickup',
                      address: ride.pickupAddress,
                    ),
                    const SizedBox(height: 16),
                    
                    // Dropoff
                    _buildLocationRow(
                      icon: Icons.circle,
                      iconColor: AppColors.error,
                      label: 'Dropoff',
                      address: ride.dropoffAddress,
                    ),
                    const SizedBox(height: 32),
                    
                    // Rider Info
                    Container(
                      padding: const EdgeInsets.all(16),
                      decoration: BoxDecoration(
                        color: Colors.grey[100],
                        borderRadius: BorderRadius.circular(16),
                      ),
                      child: Row(
                        children: [
                          const CircleAvatar(
                            radius: 24,
                            child: Icon(Icons.person, size: 28),
                          ),
                          const SizedBox(width: 16),
                          Expanded(
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Text(
                                  ride.riderName,
                                  style: const TextStyle(
                                    fontSize: 18,
                                    fontWeight: FontWeight.bold,
                                  ),
                                ),
                                Row(
                                  children: [
                                    const Icon(
                                      Icons.star,
                                      size: 16,
                                      color: Colors.amber,
                                    ),
                                    const SizedBox(width: 4),
                                    Text(
                                      ride.riderRating.toStringAsFixed(1),
                                      style: const TextStyle(fontSize: 14),
                                    ),
                                  ],
                                ),
                              ],
                            ),
                          ),
                        ],
                      ),
                    ),
                    const Spacer(),
                    
                    // Action Buttons
                    Row(
                      children: [
                        Expanded(
                          child: OutlinedButton(
                            onPressed: () {
                              _timer?.cancel();
                              context.pop();
                            },
                            style: OutlinedButton.styleFrom(
                              padding: const EdgeInsets.symmetric(vertical: 18),
                              side: const BorderSide(
                                color: Colors.red,
                                width: 2,
                              ),
                            ),
                            child: const Text(
                              'Decline',
                              style: TextStyle(
                                fontSize: 18,
                                color: Colors.red,
                                fontWeight: FontWeight.bold,
                              ),
                            ),
                          ),
                        ),
                        const SizedBox(width: 16),
                        Expanded(
                          child: ElevatedButton(
                            onPressed: () {
                              _timer?.cancel();
                              context.read<ActiveRideBloc>().add(
                                    AcceptRideRequested(rideId: ride.id),
                                  );
                              context.push('/ride/navigate/${ride.id}');
                            },
                            style: ElevatedButton.styleFrom(
                              padding: const EdgeInsets.symmetric(vertical: 18),
                              backgroundColor: AppColors.primary,
                            ),
                            child: const Text(
                              'Accept',
                              style: TextStyle(
                                fontSize: 18,
                                color: Colors.white,
                                fontWeight: FontWeight.bold,
                              ),
                            ),
                          ),
                        ),
                      ],
                    ),
                  ],
                ),
              ),
            ),
            const SizedBox(height: 24),
          ],
        ),
      ),
    );
  }

  Widget _buildLocationRow({
    required IconData icon,
    required Color iconColor,
    required String label,
    required String address,
  }) {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Icon(icon, color: iconColor, size: 20),
        const SizedBox(width: 12),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                label,
                style: TextStyle(
                  fontSize: 12,
                  color: Colors.grey[600],
                  fontWeight: FontWeight.w600,
                ),
              ),
              const SizedBox(height: 4),
              Text(
                address,
                style: const TextStyle(
                  fontSize: 16,
                  fontWeight: FontWeight.w500,
                ),
              ),
            ],
          ),
        ),
      ],
    );
  }
}