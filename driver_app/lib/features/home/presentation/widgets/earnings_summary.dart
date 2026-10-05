import 'package:flutter/material.dart';

import '../../../../core/theme/app_colors.dart';

class EarningsSummary extends StatelessWidget {
  final double todayEarnings;
  final int todayRides;
  final double onlineHours;

  const EarningsSummary({
    super.key,
    required this.todayEarnings,
    required this.todayRides,
    required this.onlineHours,
  });

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(20),
      decoration: BoxDecoration(
        gradient: LinearGradient(
          colors: [
            AppColors.primary,
            AppColors.primary.withOpacity(0.8),
          ],
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
        ),
        borderRadius: BorderRadius.circular(16),
        boxShadow: [
          BoxShadow(
            color: AppColors.primary.withOpacity(0.3),
            blurRadius: 12,
            offset: const Offset(0, 4),
          ),
        ],
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            mainAxisAlignment: MainAxisAlignment.spaceBetween,
            children: [
              const Text(
                "Today's Earnings",
                style: TextStyle(
                  color: Colors.white70,
                  fontSize: 14,
                ),
              ),
              Icon(
                Icons.trending_up,
                color: Colors.white70,
                size: 20,
              ),
            ],
          ),
          const SizedBox(height: 8),
          Text(
            '\$${todayEarnings.toStringAsFixed(2)}',
            style: const TextStyle(
              color: Colors.white,
              fontSize: 36,
              fontWeight: FontWeight.bold,
            ),
          ),
          const SizedBox(height: 16),
          Row(
            children: [
              _buildStat(
                icon: Icons.directions_car,
                label: 'Rides',
                value: todayRides.toString(),
              ),
              const SizedBox(width: 24),
              _buildStat(
                icon: Icons.access_time,
                label: 'Hours',
                value: onlineHours.toStringAsFixed(1),
              ),
              const SizedBox(width: 24),
              _buildStat(
                icon: Icons.attach_money,
                label: 'Avg/Ride',
                value: todayRides > 0
                    ? '\$${(todayEarnings / todayRides).toStringAsFixed(0)}'
                    : '\$0',
              ),
            ],
          ),
        ],
      ),
    );
  }

  Widget _buildStat({
    required IconData icon,
    required String label,
    required String value,
  }) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Icon(icon, color: Colors.white70, size: 16),
            const SizedBox(width: 4),
            Text(
              label,
              style: const TextStyle(
                color: Colors.white70,
                fontSize: 12,
              ),
            ),
          ],
        ),
        const SizedBox(height: 4),
        Text(
          value,
          style: const TextStyle(
            color: Colors.white,
            fontSize: 18,
            fontWeight: FontWeight.bold,
          ),
        ),
      ],
    );
  }
}