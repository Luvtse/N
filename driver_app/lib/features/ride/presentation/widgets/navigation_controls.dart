import 'package:flutter/material.dart';

import '../../../../core/theme/app_colors.dart';

class NavigationControls extends StatelessWidget {
  final String status;
  final VoidCallback onNavigateToPickup;
  final VoidCallback onStartRide;
  final VoidCallback onCompleteRide;

  const NavigationControls({
    super.key,
    required this.status,
    required this.onNavigateToPickup,
    required this.onStartRide,
    required this.onCompleteRide,
  });

  @override
  Widget build(BuildContext context) {
    switch (status) {
      case 'accepted':
        return ElevatedButton.icon(
          onPressed: onNavigateToPickup,
          icon: const Icon(Icons.navigation),
          label: const Text('Navigate to Pickup'),
          style: ElevatedButton.styleFrom(
            padding: const EdgeInsets.symmetric(vertical: 16),
            backgroundColor: AppColors.primary,
          ),
        );
      
      case 'arrived':
        return ElevatedButton.icon(
          onPressed: onStartRide,
          icon: const Icon(Icons.play_arrow),
          label: const Text('Start Ride'),
          style: ElevatedButton.styleFrom(
            padding: const EdgeInsets.symmetric(vertical: 16),
            backgroundColor: AppColors.success,
          ),
        );
      
      case 'in_progress':
        return ElevatedButton.icon(
          onPressed: onCompleteRide,
          icon: const Icon(Icons.check),
          label: const Text('Complete Ride'),
          style: ElevatedButton.styleFrom(
            padding: const EdgeInsets.symmetric(vertical: 16),
            backgroundColor: AppColors.success,
          ),
        );
      
      default:
        return const SizedBox.shrink();
    }
  }
}