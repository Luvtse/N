import 'package:flutter/material.dart';

import '../../../../core/theme/app_colors.dart';

class RiderInfoCard extends StatelessWidget {
  final String riderName;
  final double riderRating;
  final String pickupAddress;
  final String dropoffAddress;

  const RiderInfoCard({
    super.key,
    required this.riderName,
    required this.riderRating,
    required this.pickupAddress,
    required this.dropoffAddress,
  });

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: Colors.grey[50],
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: Colors.grey[200]!),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          // Rider Info
          Row(
            children: [
              const CircleAvatar(
                radius: 24,
                backgroundColor: AppColors.primary,
                child: Icon(Icons.person, color: Colors.white, size: 28),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      riderName,
                      style: const TextStyle(
                        fontSize: 18,
                        fontWeight: FontWeight.bold,
                      ),
                    ),
                    Row(
                      children: [
                        const Icon(Icons.star, size: 16, color: Colors.amber),
                        const SizedBox(width: 4),
                        Text(
                          riderRating.toStringAsFixed(1),
                          style: const TextStyle(
                            fontSize: 14,
                            fontWeight: FontWeight.w600,
                          ),
                        ),
                      ],
                    ),
                  ],
                ),
              ),
              IconButton(
                icon: const Icon(Icons.call, color: AppColors.primary),
                onPressed: () {
                  // TODO: Call rider
                },
              ),
              IconButton(
                icon: const Icon(Icons.message, color: AppColors.primary),
                onPressed: () {
                  // TODO: Message rider
                },
              ),
            ],
          ),
          const SizedBox(height: 16),
          const Divider(),
          const SizedBox(height: 16),
          
          // Pickup
          _buildLocationRow(
            icon: Icons.circle,
            iconColor: AppColors.success,
            label: 'Pickup',
            address: pickupAddress,
          ),
          const SizedBox(height: 12),
          
          // Dropoff
          _buildLocationRow(
            icon: Icons.circle,
            iconColor: AppColors.error,
            label: 'Dropoff',
            address: dropoffAddress,
          ),
        ],
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
        Icon(icon, color: iconColor, size: 16),
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
              const SizedBox(height: 2),
              Text(
                address,
                style: const TextStyle(fontSize: 14),
              ),
            ],
          ),
        ),
      ],
    );
  }
}