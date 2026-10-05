import 'package:flutter/material.dart';
import 'package:fl_chart/fl_chart.dart';
import '../../../../core/theme/app_colors.dart';

class ShipmentTrackingPage extends StatelessWidget {
  final String shipmentId;

  const ShipmentTrackingPage({super.key, required this.shipmentId});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Shipment Tracking'),
        actions: [
          IconButton(
            icon: const Icon(Icons.share),
            onPressed: () {},
          ),
        ],
      ),
      body: SingleChildScrollView(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            // Map placeholder
            Container(
              height: 250,
              color: Colors.grey[200],
              child: Stack(
                children: [
                  const Center(child: Text('Map View')),
                  Positioned(
                    top: 16,
                    left: 16,
                    right: 16,
                    child: Container(
                      padding: const EdgeInsets.all(16),
                      decoration: BoxDecoration(
                        color: Colors.white,
                        borderRadius: BorderRadius.circular(12),
                        boxShadow: [
                          BoxShadow(
                            color: Colors.black.withOpacity(0.1),
                            blurRadius: 8,
                          ),
                        ],
                      ),
                      child: Row(
                        children: [
                          Container(
                            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
                            decoration: BoxDecoration(
                              color: AppColors.success,
                              borderRadius: BorderRadius.circular(8),
                            ),
                            child: const Text(
                              'In Transit',
                              style: TextStyle(color: Colors.white, fontWeight: FontWeight.bold),
                            ),
                          ),
                          const Spacer(),
                          const Text(
                            'ETA: 2 days',
                            style: TextStyle(fontWeight: FontWeight.w600),
                          ),
                        ],
                      ),
                    ),
                  ),
                ],
              ),
            ),

            Padding(
              padding: const EdgeInsets.all(16),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  // Shipment Info
                  _buildShipmentInfo(),
                  const SizedBox(height: 20),

                  // Route Progress
                  _buildRouteProgress(),
                  const SizedBox(height: 20),

                  // Documents
                  _buildDocuments(),
                  const SizedBox(height: 20),

                  // Customs Status
                  _buildCustomsStatus(),
                  const SizedBox(height: 20),

                  // Tracking Timeline
                  _buildTrackingTimeline(),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildShipmentInfo() {
    return Container(
      padding: const EdgeInsets.all(20),
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: Colors.grey[200]!),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text(
            'Shipment Details',
            style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold),
          ),
          const SizedBox(height: 16),
          _buildInfoRow('Tracking Number', 'LX-2026-78542'),
          _buildInfoRow('Container', 'MSKU-1234567'),
          _buildInfoRow('Bill of Lading', 'BOL-78542-2026'),
          _buildInfoRow('Carrier', 'FastFreight Logistics'),
          _buildInfoRow('Service Type', 'Ocean FCL'),
          const Divider(height: 24),
          Row(
            children: [
              Expanded(
                child: _buildLocationBlock(
                  label: 'Origin',
                  city: 'Shanghai',
                  country: 'China',
                  icon: Icons.flight_takeoff,
                  color: AppColors.primary,
                ),
              ),
              const Icon(Icons.arrow_forward, color: AppColors.primary),
              Expanded(
                child: _buildLocationBlock(
                  label: 'Destination',
                  city: 'Los Angeles',
                  country: 'USA',
                  icon: Icons.flight_land,
                  color: AppColors.success,
                ),
              ),
            ],
          ),
          const Divider(height: 24),
          Row(
            children: [
              _buildStatBox('Weight', '12,500 kg'),
              const SizedBox(width: 12),
              _buildStatBox('Volume', '28.5 CBM'),
              const SizedBox(width: 12),
              _buildStatBox('Packages', '42'),
            ],
          ),
        ],
      ),
    );
  }

  Widget _buildInfoRow(String label, String value) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: Row(
        mainAxisAlignment: MainAxisAlignment.spaceBetween,
        children: [
          Text(label, style: TextStyle(color: Colors.grey[600])),
          Text(value, style: const TextStyle(fontWeight: FontWeight.w600)),
        ],
      ),
    );
  }

  Widget _buildLocationBlock({
    required String label,
    required String city,
    required String country,
    required IconData icon,
    required Color color,
  }) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(label, style: TextStyle(color: Colors.grey[600], fontSize: 12)),
        const SizedBox(height: 4),
        Row(
          children: [
            Icon(icon, color: color, size: 20),
            const SizedBox(width: 4),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(city, style: const TextStyle(fontWeight: FontWeight.bold)),
                  Text(country, style: TextStyle(color: Colors.grey[600], fontSize: 12)),
                ],
              ),
            ),
          ],
        ),
      ],
    );
  }

  Widget _buildStatBox(String label, String value) {
    return Expanded(
      child: Container(
        padding: const EdgeInsets.all(12),
        decoration: BoxDecoration(
          color: AppColors.logixGreen.withOpacity(0.1),
          borderRadius: BorderRadius.circular(8),
        ),
        child: Column(
          children: [
            Text(value, style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 16)),
            const SizedBox(height: 4),
            Text(label, style: TextStyle(color: Colors.grey[600], fontSize: 12)),
          ],
        ),
      ),
    );
  }

  Widget _buildRouteProgress() {
    return Container(
      padding: const EdgeInsets.all(20),
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: Colors.grey[200]!),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text(
            'Route Progress',
            style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold),
          ),
          const SizedBox(height: 20),
          SizedBox(
            height: 100,
            child: LineChart(
              LineChartData(
                gridData: const FlGridData(show: false),
                titlesData: const FlTitlesData(show: false),
                borderData: FlBorderData(show: false),
                lineBarsData: [
                  LineChartBarData(
                    spots: const [
                      FlSpot(0, 0),
                      FlSpot(1, 15),
                      FlSpot(2, 30),
                      FlSpot(3, 45),
                      FlSpot(4, 62),
                      FlSpot(5, 78),
                    ],
                    isCurved: true,
                    color: AppColors.logixGreen,
                    barWidth: 4,
                    dotData: const FlDotData(show: true),
                    belowBarData: BarAreaData(
                      show: true,
                      color: AppColors.logixGreen.withOpacity(0.1),
                    ),
                  ),
                ],
              ),
            ),
          ),
          const SizedBox(height: 16),
          Row(
            mainAxisAlignment: MainAxisAlignment.spaceBetween,
            children: [
              _buildProgressStat('Distance', '8,500 km', '6,200 km'),
              _buildProgressStat('Time', '14 days', 'Day 9'),
              _buildProgressStat('Progress', '', '73%'),
            ],
          ),
        ],
      ),
    );
  }

  Widget _buildProgressStat(String label, String total, String current) {
    return Column(
      children: [
        Text(current, style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 18)),
        const SizedBox(height: 4),
        Text(label, style: TextStyle(color: Colors.grey[600], fontSize: 12)),
        if (total.isNotEmpty)
          Text(total, style: TextStyle(color: Colors.grey[400], fontSize: 10)),
      ],
    );
  }

  Widget _buildDocuments() {
    return Container(
      padding: const EdgeInsets.all(20),
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: Colors.grey[200]!),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text(
            'Documents',
            style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold),
          ),
          const SizedBox(height: 16),
          _buildDocumentItem('Commercial Invoice', 'invoice_78542.pdf', Icons.description),
          _buildDocumentItem('Packing List', 'packing_78542.pdf', Icons.list_alt),
          _buildDocumentItem('Bill of Lading', 'bol_78542.pdf', Icons.receipt_long),
          _buildDocumentItem('Certificate of Origin', 'coo_78542.pdf', Icons.workspace_premium),
        ],
      ),
    );
  }

  Widget _buildDocumentItem(String title, String filename, IconData icon) {
    return Container(
      margin: const EdgeInsets.only(bottom: 8),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: Colors.grey[50],
        borderRadius: BorderRadius.circular(8),
      ),
      child: Row(
        children: [
          Icon(icon, color: AppColors.primary),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(title, style: const TextStyle(fontWeight: FontWeight.w600)),
                Text(filename, style: TextStyle(color: Colors.grey[600], fontSize: 12)),
              ],
            ),
          ),
          IconButton(
            icon: const Icon(Icons.download, color: AppColors.primary),
            onPressed: () {},
          ),
        ],
      ),
    );
  }

  Widget _buildCustomsStatus() {
    return Container(
      padding: const EdgeInsets.all(20),
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: Colors.grey[200]!),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text(
            'Customs Clearance',
            style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold),
          ),
          const SizedBox(height: 16),
          Container(
            padding: const EdgeInsets.all(16),
            decoration: BoxDecoration(
              color: AppColors.warning.withOpacity(0.1),
              borderRadius: BorderRadius.circular(12),
              border: Border.all(color: AppColors.warning.withOpacity(0.3)),
            ),
            child: Row(
              children: [
                const Icon(Icons.hourglass_top, color: AppColors.warning, size: 32),
                const SizedBox(width: 16),
                const Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        'Under Review',
                        style: TextStyle(fontWeight: FontWeight.bold, fontSize: 16),
                      ),
                      SizedBox(height: 4),
                      Text(
                        'Expected clearance: July 12, 2026',
                        style: TextStyle(color: AppColors.warning),
                      ),
                    ],
                  ),
                ),
              ],
            ),
          ),
          const SizedBox(height: 16),
          _buildInfoRow('Duties Estimated', '\$2,450.00'),
          _buildInfoRow('Taxes Estimated', '\$890.00'),
          _buildInfoRow('HS Code', '8471.30.0100'),
          _buildInfoRow('Declaration ID', 'CUS-2026-98542'),
        ],
      ),
    );
  }

  Widget _buildTrackingTimeline() {
    return Container(
      padding: const EdgeInsets.all(20),
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: Colors.grey[200]!),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text(
            'Tracking Timeline',
            style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold),
          ),
          const SizedBox(height: 16),
          _buildTimelineItem(
            title: 'Shipment Booked',
            subtitle: 'July 1, 2026 • 09:30 AM',
            icon: Icons.bookmark,
            color: AppColors.success,
            isCompleted: true,
          ),
          _buildTimelineItem(
            title: 'Picked Up from Origin',
            subtitle: 'July 2, 2026 • 02:15 PM',
            icon: Icons.local_shipping,
            color: AppColors.success,
            isCompleted: true,
          ),
          _buildTimelineItem(
            title: 'Departed Shanghai Port',
            subtitle: 'July 4, 2026 • 08:00 AM',
            icon: Icons.directions_boat,
            color: AppColors.success,
            isCompleted: true,
          ),
          _buildTimelineItem(
            title: 'In Transit - Pacific Ocean',
            subtitle: 'Current location',
            icon: Icons.map,
            color: AppColors.primary,
            isCompleted: false,
            isActive: true,
          ),
          _buildTimelineItem(
            title: 'Arrive at Los Angeles Port',
            subtitle: 'Estimated: July 11, 2026',
            icon: Icons.anchor,
            color: Colors.grey,
            isCompleted: false,
          ),
          _buildTimelineItem(
            title: 'Customs Clearance',
            subtitle: 'Estimated: July 12, 2026',
            icon: Icons.verified_user,
            color: Colors.grey,
            isCompleted: false,
          ),
          _buildTimelineItem(
            title: 'Out for Delivery',
            subtitle: 'Estimated: July 13, 2026',
            icon: Icons.local_shipping,
            color: Colors.grey,
            isCompleted: false,
          ),
          _buildTimelineItem(
            title: 'Delivered',
            subtitle: 'Estimated: July 13, 2026',
            icon: Icons.check_circle,
            color: Colors.grey,
            isCompleted: false,
          ),
        ],
      ),
    );
  }

  Widget _buildTimelineItem({
    required String title,
    required String subtitle,
    required IconData icon,
    required Color color,
    required bool isCompleted,
    bool isActive = false,
  }) {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Column(
          children: [
            Container(
              width: 40,
              height: 40,
              decoration: BoxDecoration(
                color: isCompleted || isActive ? color : Colors.grey[300],
                shape: BoxShape.circle,
                border: isActive ? Border.all(color: color, width: 3) : null,
              ),
              child: Icon(icon, color: Colors.white, size: 20),
            ),
            if (!isActive)
              Container(
                width: 2,
                height: 40,
                color: isCompleted ? color : Colors.grey[300],
              ),
          ],
        ),
        const SizedBox(width: 16),
        Expanded(
          child: Padding(
            padding: const EdgeInsets.only(bottom: 16),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  title,
                  style: TextStyle(
                    fontWeight: FontWeight.w600,
                    color: isCompleted || isActive ? Colors.black : Colors.grey,
                  ),
                ),
                const SizedBox(height: 4),
                Text(
                  subtitle,
                  style: TextStyle(color: Colors.grey[600], fontSize: 12),
                ),
              ],
            ),
          ),
        ),
      ],
    );
  }
}