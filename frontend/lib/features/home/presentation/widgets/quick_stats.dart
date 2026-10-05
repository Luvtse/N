import 'package:flutter/material.dart';

class QuickStat {
  final IconData icon;
  final String label;
  final String value;
  final Color color;

  const QuickStat({
    required this.icon,
    required this.label,
    required this.value,
    required this.color,
  });
}

class QuickStats extends StatelessWidget {
  final List<QuickStat> stats;

  const QuickStats({super.key, required this.stats});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(20),
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.circular(20),
        boxShadow: [
          BoxShadow(
            color: Colors.black.withOpacity(0.04),
            blurRadius: 10,
            offset: const Offset(0, 2),
          ),
        ],
      ),
      child: Row(
        children: stats.map((stat) => _buildStat(stat)).toList(),
      ),
    );
  }

  Widget _buildStat(QuickStat stat) {
    return Expanded(
      child: Column(
        children: [
          Container(
            width: 48,
            height: 48,
            decoration: BoxDecoration(
              color: stat.color.withOpacity(0.1),
              borderRadius: BorderRadius.circular(12),
            ),
            child: Icon(stat.icon, color: stat.color, size: 24),
          ),
          const SizedBox(height: 12),
          Text(
            stat.value,
            style: TextStyle(
              fontSize: 20,
              fontWeight: FontWeight.bold,
              color: stat.color,
            ),
          ),
          const SizedBox(height: 4),
          Text(
            stat.label,
            style: TextStyle(
              fontSize: 12,
              color: Colors.grey[600],
            ),
          ),
        ],
      ),
    );
  }
}