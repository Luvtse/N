import 'package:flutter/material.dart';

import '../../../../core/theme/app_colors.dart';

class RideTypeSelector extends StatelessWidget {
  final String selectedType;
  final ValueChanged<String> onTypeSelected;

  const RideTypeSelector({
    super.key,
    required this.selectedType,
    required this.onTypeSelected,
  });

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        Expanded(
          child: _buildRideOption(
            type: 'standard',
            icon: Icons.directions_car,
            title: 'Standard',
            price: '\$12-15',
            isSelected: selectedType == 'standard',
            onTap: () => onTypeSelected('standard'),
          ),
        ),
        const SizedBox(width: 12),
        Expanded(
          child: _buildRideOption(
            type: 'premium',
            icon: Icons.star,
            title: 'Premium',
            price: '\$20-25',
            isSelected: selectedType == 'premium',
            onTap: () => onTypeSelected('premium'),
          ),
        ),
        const SizedBox(width: 12),
        Expanded(
          child: _buildRideOption(
            type: 'electric',
            icon: Icons.electric_car,
            title: 'Electric',
            price: '\$14-18',
            isSelected: selectedType == 'electric',
            onTap: () => onTypeSelected('electric'),
          ),
        ),
      ],
    );
  }

  Widget _buildRideOption({
    required String type,
    required IconData icon,
    required String title,
    required String price,
    required bool isSelected,
    required VoidCallback onTap,
  }) {
    return GestureDetector(
      onTap: onTap,
      child: Container(
        padding: const EdgeInsets.all(16),
        decoration: BoxDecoration(
          color: isSelected 
              ? AppColors.primary.withOpacity(0.1) 
              : AppColors.surfaceVariant,
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