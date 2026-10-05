import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:go_router/go_router.dart';

import '../../../../core/theme/app_colors.dart';
import '../../domain/entities/active_ride.dart';
import '../bloc/active_ride_bloc.dart';

class RideCompletionPage extends StatefulWidget {
  final String rideId;

  const RideCompletionPage({
    super.key,
    required this.rideId,
  });

  @override
  State<RideCompletionPage> createState() => _RideCompletionPageState();
}

class _RideCompletionPageState extends State<RideCompletionPage> {
  double _tipAmount = 0;
  final List<double> _tipOptions = [0, 2, 5, 10, 20];

  @override
  Widget build(BuildContext context) {
    // TODO: Load actual ride data
    final ride = ActiveRide(
      id: widget.rideId,
      riderId: 'rider_123',
      riderName: 'John Doe',
      riderPhone: '+1234567890',
      riderRating: 4.8,
      pickupLat: 40.7128,
      pickupLng: -74.0060,
      pickupAddress: '123 Broadway, NYC',
      dropoffLat: 40.7589,
      dropoffLng: -73.9851,
      dropoffAddress: '456 5th Ave, NYC',
      distanceKm: 5.2,
      estimatedDurationMinutes: 18,
      fareAmount: 15.50,
      tipAmount: 0,
      status: 'in_progress',
      requestedAt: DateTime.now(),
    );

    return Scaffold(
      appBar: AppBar(
        title: const Text('Complete Ride'),
      ),
      body: SingleChildScrollView(
        padding: const EdgeInsets.all(24),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            // Success Icon
            const Icon(
              Icons.check_circle,
              size: 100,
              color: AppColors.success,
            ),
            const SizedBox(height: 24),
            const Text(
              'Ride Completed!',
              style: TextStyle(
                fontSize: 28,
                fontWeight: FontWeight.bold,
              ),
              textAlign: TextAlign.center,
            ),
            const SizedBox(height: 32),
            
            // Earnings Summary
            Container(
              padding: const EdgeInsets.all(20),
              decoration: BoxDecoration(
                color: AppColors.primary.withOpacity(0.1),
                borderRadius: BorderRadius.circular(16),
              ),
              child: Column(
                children: [
                  _buildEarningsRow(
                    'Base Fare',
                    '\$${ride.fareAmount.toStringAsFixed(2)}',
                  ),
                  const Divider(height: 24),
                  _buildEarningsRow(
                    'Tip',
                    '\$${_tipAmount.toStringAsFixed(2)}',
                  ),
                  const Divider(height: 24),
                  _buildEarningsRow(
                    'Total Earnings',
                    '\$${(ride.fareAmount + _tipAmount).toStringAsFixed(2)}',
                    isTotal: true,
                  ),
                ],
              ),
            ),
            const SizedBox(height: 32),
            
            // Tip Selection
            const Text(
              'Add a tip?',
              style: TextStyle(
                fontSize: 20,
                fontWeight: FontWeight.bold,
              ),
              textAlign: TextAlign.center,
            ),
            const SizedBox(height: 16),
            Wrap(
              spacing: 12,
              runSpacing: 12,
              alignment: WrapAlignment.center,
              children: _tipOptions.map((amount) {
                final isSelected = _tipAmount == amount;
                return ChoiceChip(
                  label: Text(
                    amount == 0 ? 'No tip' : '\$$amount',
                  ),
                  selected: isSelected,
                  onSelected: (selected) {
                    if (selected) {
                      setState(() => _tipAmount = amount);
                    }
                  },
                  selectedColor: AppColors.primary,
                  labelStyle: TextStyle(
                    color: isSelected ? Colors.white : AppColors.textPrimary,
                    fontWeight: FontWeight.w600,
                  ),
                );
              }).toList(),
            ),
            const SizedBox(height: 48),
            
            // Complete Button
            ElevatedButton(
              onPressed: () => _completeRide(),
              style: ElevatedButton.styleFrom(
                padding: const EdgeInsets.symmetric(vertical: 18),
                backgroundColor: AppColors.success,
              ),
              child: const Text(
                'Complete & Return Home',
                style: TextStyle(
                  fontSize: 18,
                  fontWeight: FontWeight.bold,
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildEarningsRow(
    String label,
    String amount, {
    bool isTotal = false,
  }) {
    return Row(
      mainAxisAlignment: MainAxisAlignment.spaceBetween,
      children: [
        Text(
          label,
          style: TextStyle(
            fontSize: isTotal ? 18 : 16,
            fontWeight: isTotal ? FontWeight.bold : FontWeight.normal,
          ),
        ),
        Text(
          amount,
          style: TextStyle(
            fontSize: isTotal ? 24 : 18,
            fontWeight: FontWeight.bold,
            color: isTotal ? AppColors.primary : null,
          ),
        ),
      ],
    );
  }

  void _completeRide() {
    context.read<ActiveRideBloc>().add(
          CompleteRideRequested(
            rideId: widget.rideId,
            tipAmount: _tipAmount,
          ),
        );
    
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(
          'Ride completed! You earned \$${(15.50 + _tipAmount).toStringAsFixed(2)}',
        ),
        backgroundColor: AppColors.success,
      ),
    );
    
    context.go('/home');
  }
}