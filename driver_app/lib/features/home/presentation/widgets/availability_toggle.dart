import 'package:flutter/material.dart';
import '../../../../core/theme/app_colors.dart';

class AvailabilityToggle extends StatelessWidget {
  final bool isOnline;
  final ValueChanged<bool> onChanged;

  const AvailabilityToggle({
    super.key,
    required this.isOnline,
    required this.onChanged,
  });

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(20),
      decoration: BoxDecoration(
        gradient: LinearGradient(
          colors: isOnline
              ? [AppColors.success, AppColors.success.withOpacity(0.7)]
              : [Colors.grey[400]!, Colors.grey[300]!],
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
        ),
      ),
      child: Row(
        mainAxisAlignment: MainAxisAlignment.spaceBetween,
        children: [
          Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                isOnline ? 'You are online' : 'You are offline',
                style: const TextStyle(
                  color: Colors.white,
                  fontSize: 18,
                  fontWeight: FontWeight.bold,
                ),
              ),
              const SizedBox(height: 4),
              Text(
                isOnline ? 'Ready to accept rides' : 'Go online to start earning',
                style: const TextStyle(
                  color: Colors.white70,
                  fontSize: 14,
                ),
              ),
            ],
          ),
          Switch(
            value: isOnline,
            onChanged: onChanged,
            activeColor: Colors.white,
            activeTrackColor: Colors.white30,
          ),
        ],
      ),
    );
  }
}