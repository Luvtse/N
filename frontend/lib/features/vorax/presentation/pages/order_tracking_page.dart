import 'dart:async';
import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

import '../../../../core/theme/app_colors.dart';

class OrderTrackingPage extends StatefulWidget {
  final String orderId;

  const OrderTrackingPage({super.key, required this.orderId});

  @override
  State<OrderTrackingPage> createState() => _OrderTrackingPageState();
}

class _OrderTrackingPageState extends State<OrderTrackingPage> {
  String _currentStatus = 'preparing';
  int _estimatedMinutes = 25;
  Timer? _timer;

  @override
  void initState() {
    super.initState();
    _startStatusUpdates();
  }

  void _startStatusUpdates() {
    _timer = Timer.periodic(const Duration(seconds: 30), (timer) {
      setState(() {
        if (_estimatedMinutes > 0) {
          _estimatedMinutes--;
        }
      });
    });
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: AppColors.background,
      appBar: AppBar(
        title: const Text('Order Tracking'),
        backgroundColor: Colors.white,
        elevation: 0,
        leading: IconButton(
          icon: const Icon(Icons.arrow_back),
          onPressed: () => context.pop(),
        ),
      ),
      body: SingleChildScrollView(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            // Status card
            Container(
              padding: const EdgeInsets.all(24),
              decoration: BoxDecoration(
                gradient: AppColors.voraxGradient,
                borderRadius: BorderRadius.circular(20),
                boxShadow: [
                  BoxShadow(
                    color: AppColors.voraxOrange.withOpacity(0.3),
                    blurRadius: 20,
                    offset: const Offset(0, 10),
                  ),
                ],
              ),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const Text(
                    'Estimated Delivery',
                    style: TextStyle(color: Colors.white70, fontSize: 14),
                  ),
                  const SizedBox(height: 8),
                  Text(
                    '$_estimatedMinutes min',
                    style: const TextStyle(
                      color: Colors.white,
                      fontSize: 48,
                      fontWeight: FontWeight.bold,
                    ),
                  ),
                  const SizedBox(height: 16),
                  LinearProgressIndicator(
                    value: (25 - _estimatedMinutes) / 25,
                    backgroundColor: Colors.white30,
                    valueColor: const AlwaysStoppedAnimation<Color>(Colors.white),
                    minHeight: 8,
                  ),
                ],
              ),
            ),

            const SizedBox(height: 24),

            // Order status
            const Text(
              'Order Status',
              style: TextStyle(fontSize: 20, fontWeight: FontWeight.bold),
            ),
            const SizedBox(height: 16),
            _buildStatusTimeline(),

            const SizedBox(height: 24),

            // Delivery details
            const Text(
              'Delivery Details',
              style: TextStyle(fontSize: 20, fontWeight: FontWeight.bold),
            ),
            const SizedBox(height: 16),
            _buildDeliveryDetails(),

            const SizedBox(height: 24),

            // Driver info
            if (_currentStatus == 'in_transit') _buildDriverInfo(),
          ],
        ),
      ),
    );
  }

  Widget _buildStatusTimeline() {
    final statuses = [
      {'status': 'confirmed', 'label': 'Order Confirmed', 'icon': Icons.check_circle},
      {'status': 'preparing', 'label': 'Preparing', 'icon': Icons.restaurant},
      {'status': 'ready', 'label': 'Ready for Pickup', 'icon': Icons.delivery_dining},
      {'status': 'in_transit', 'label': 'On the Way', 'icon': Icons.directions_bike},
      {'status': 'delivered', 'label': 'Delivered', 'icon': Icons.home},
    ];

    final currentIndex = statuses.indexWhere((s) => s['status'] == _currentStatus);

    return Column(
      children: statuses.asMap().entries.map((entry) {
        final index = entry.key;
        final status = entry.value;
        final isCompleted = index <= currentIndex;
        final isCurrent = index == currentIndex;

        return Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Column(
              children: [
                Container(
                  width: 40,
                  height: 40,
                  decoration: BoxDecoration(
                    color: isCompleted ? AppColors.voraxOrange : Colors.grey[300],
                    shape: BoxShape.circle,
                    border: isCurrent
                        ? Border.all(color: AppColors.voraxOrange, width: 3)
                        : null,
                  ),
                  child: Icon(
                    status['icon'] as IconData,
                    color: isCompleted ? Colors.white : Colors.grey,
                    size: 20,
                  ),
                ),
                if (index < statuses.length - 1)
                  Container(
                    width: 2,
                    height: 40,
                    color: isCompleted ? AppColors.voraxOrange : Colors.grey[300],
                  ),
              ],
            ),
            const SizedBox(width: 16),
            Expanded(
              child: Padding(
                padding: const EdgeInsets.only(top: 8),
                child: Text(
                  status['label'] as String,
                  style: TextStyle(
                    fontSize: 16,
                    fontWeight: isCurrent ? FontWeight.bold : FontWeight.normal,
                    color: isCompleted ? AppColors.textPrimary : Colors.grey,
                  ),
                ),
              ),
            ),
          ],
        );
      }).toList(),
    );
  }

  Widget _buildDeliveryDetails() {
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.circular(16),
        boxShadow: [
          BoxShadow(
            color: Colors.black.withOpacity(0.05),
            blurRadius: 10,
            offset: const Offset(0, 2),
          ),
        ],
      ),
      child: Column(
        children: [
          _buildDetailRow(
            icon: Icons.store,
            label: 'Restaurant',
            value: 'Pizza Palace',
          ),
          const Divider(),
          _buildDetailRow(
            icon: Icons.location_on,
            label: 'Delivery Address',
            value: '123 Main St, Apt 4B',
          ),
          const Divider(),
          _buildDetailRow(
            icon: Icons.receipt,
            label: 'Order ID',
            value: '#${widget.orderId.substring(0, 8)}',
          ),
        ],
      ),
    );
  }

  Widget _buildDetailRow({
    required IconData icon,
    required String label,
    required String value,
  }) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 12),
      child: Row(
        children: [
          Icon(icon, color: AppColors.voraxOrange, size: 24),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  label,
                  style: TextStyle(fontSize: 12, color: Colors.grey[600]),
                ),
                const SizedBox(height: 4),
                Text(
                  value,
                  style: const TextStyle(
                    fontSize: 14,
                    fontWeight: FontWeight.w600,
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildDriverInfo() {
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.circular(16),
        boxShadow: [
          BoxShadow(
            color: Colors.black.withOpacity(0.05),
            blurRadius: 10,
            offset: const Offset(0, 2),
          ),
        ],
      ),
      child: Row(
        children: [
          const CircleAvatar(
            radius: 30,
            backgroundImage: NetworkImage('https://i.pravatar.cc/150?img=12'),
          ),
          const SizedBox(width: 16),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const Text(
                  'Mike Johnson',
                  style: TextStyle(fontSize: 16, fontWeight: FontWeight.bold),
                ),
                Row(
                  children: [
                    const Icon(Icons.star, color: Colors.amber, size: 16),
                    const SizedBox(width: 4),
                    const Text('4.9'),
                    const SizedBox(width: 8),
                    Text(
                      '• 1,250 deliveries',
                      style: TextStyle(color: Colors.grey[600]),
                    ),
                  ],
                ),
              ],
            ),
          ),
          IconButton(
            icon: const Icon(Icons.call, color: AppColors.voraxOrange),
            onPressed: () {},
          ),
          IconButton(
            icon: const Icon(Icons.message, color: AppColors.voraxOrange),
            onPressed: () {},
          ),
        ],
      ),
    );
  }
}