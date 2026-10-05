import 'package:flutter/material.dart';
import 'package:intl/intl.dart';

import '../../../../core/theme/app_colors.dart';

class EarningsDetailPage extends StatelessWidget {
  const EarningsDetailPage({super.key});

  @override
  Widget build(BuildContext context) {
    // TODO: Load actual earnings data
    final dailyEarnings = List.generate(
      7,
      (index) => {
        'date': DateTime.now().subtract(Duration(days: index)),
        'earnings': 50.0 + (index * 10),
        'rides': 3 + index,
      },
    );

    return Scaffold(
      appBar: AppBar(
        title: const Text('Earnings Details'),
      ),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          // Summary Card
          Container(
            padding: const EdgeInsets.all(20),
            decoration: BoxDecoration(
              color: AppColors.primary.withOpacity(0.1),
              borderRadius: BorderRadius.circular(16),
            ),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const Text(
                  'This Week',
                  style: TextStyle(
                    fontSize: 14,
                    color: AppColors.textSecondary,
                  ),
                ),
                const SizedBox(height: 8),
                const Text(
                  '\$450.00',
                  style: TextStyle(
                    fontSize: 32,
                    fontWeight: FontWeight.bold,
                    color: AppColors.primary,
                  ),
                ),
                const SizedBox(height: 16),
                Row(
                  children: [
                    _buildMiniStat('Rides', '24'),
                    const SizedBox(width: 24),
                    _buildMiniStat('Hours', '18.5'),
                    const SizedBox(width: 24),
                    _buildMiniStat('Tips', '\$45'),
                  ],
                ),
              ],
            ),
          ),
          const SizedBox(height: 24),
          
          // Daily Breakdown
          const Text(
            'Daily Breakdown',
            style: TextStyle(
              fontSize: 18,
              fontWeight: FontWeight.bold,
            ),
          ),
          const SizedBox(height: 16),
          
          ...dailyEarnings.map((day) => _buildDailyCard(day)),
        ],
      ),
    );
  }

  Widget _buildMiniStat(String label, String value) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          value,
          style: const TextStyle(
            fontSize: 18,
            fontWeight: FontWeight.bold,
          ),
        ),
        Text(
          label,
          style: const TextStyle(
            fontSize: 12,
            color: AppColors.textSecondary,
          ),
        ),
      ],
    );
  }

  Widget _buildDailyCard(Map<String, dynamic> day) {
    final date = day['date'] as DateTime;
    final earnings = day['earnings'] as double;
    final rides = day['rides'] as int;

    return Card(
      margin: const EdgeInsets.only(bottom: 12),
      child: ListTile(
        leading: Container(
          width: 48,
          height: 48,
          decoration: BoxDecoration(
            color: AppColors.primary.withOpacity(0.1),
            borderRadius: BorderRadius.circular(12),
          ),
          child: Column(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              Text(
                DateFormat('dd').format(date),
                style: const TextStyle(
                  fontSize: 16,
                  fontWeight: FontWeight.bold,
                  color: AppColors.primary,
                ),
              ),
              Text(
                DateFormat('MMM').format(date),
                style: const TextStyle(
                  fontSize: 10,
                  color: AppColors.textSecondary,
                ),
              ),
            ],
          ),
        ),
        title: Text('\$${earnings.toStringAsFixed(2)}'),
        subtitle: Text('$rides rides'),
        trailing: const Icon(Icons.chevron_right),
        onTap: () {
          // TODO: Navigate to day detail
        },
      ),
    );
  }
}