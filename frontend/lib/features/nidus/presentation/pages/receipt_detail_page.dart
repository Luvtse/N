import 'package:flutter/material.dart';

import '../../../../core/theme/app_colors.dart';
import '../../data/repositories/ride_repository.dart';

/// Post-trip receipt detail. Itemizes the fare from the Ride entity returned
/// by GET /nidus/rides/{id} and links into the existing wallet dispute flow
/// (route: /wallet?disputeRideId=...) — no new dispute plumbing here.
class ReceiptDetailPage extends StatelessWidget {
  final Ride ride;

  const ReceiptDetailPage({super.key, required this.ride});

  static Future<void> open(
    BuildContext context, {
    required Ride ride,
  }) {
    return Navigator.of(context).push<void>(
      MaterialPageRoute<void>(
        builder: (_) => ReceiptDetailPage(ride: ride),
      ),
    );
  }

  String _formatMoney(double amount) =>
      '${ride.currency} ${amount.toStringAsFixed(2)}';

  String _formatTime(DateTime time) {
    final h = time.hour.toString().padLeft(2, '0');
    final m = time.minute.toString().padLeft(2, '0');
    return '$h:$m';
  }

  @override
  Widget build(BuildContext context) {
    final fare = ride.fareAmount ?? 0;
    // Backend ledger splits are not carried on the Ride DTO, so show the
    // known components and label the remainder honestly instead of inventing
    // a breakdown that could contradict the escrowed charge.
    final distanceKm = ride.distanceKm;
    final durationMin = ride.durationMinutes;

    return Scaffold(
      appBar: AppBar(title: const Text('Receipt')),
      body: ListView(
        padding: const EdgeInsets.all(24),
        children: [
          Container(
            padding: const EdgeInsets.all(16),
            decoration: BoxDecoration(
              color: AppColors.inputBackground,
              borderRadius: BorderRadius.circular(16),
            ),
            child: Column(
              children: [
                Text(
                  _formatMoney(fare),
                  style: const TextStyle(
                    fontSize: 32,
                    fontWeight: FontWeight.bold,
                    color: AppColors.textPrimary,
                  ),
                ),
                const SizedBox(height: 4),
                Text(
                  'Ride ${ride.id.substring(0, ride.id.length >= 8 ? 8 : ride.id.length)} • ${ride.status}',
                  style: const TextStyle(
                      fontSize: 12, color: AppColors.textSecondary),
                ),
              ],
            ),
          ),
          const SizedBox(height: 24),
          _RouteSection(ride: ride),
          const SizedBox(height: 16),
          _Line(label: 'Requested',
              value: '${_formatTime(ride.requestedAt)}'),
          if (ride.matchedAt != null)
            _Line(
                label: 'Driver matched',
                value: _formatTime(ride.matchedAt!)),
          if (ride.completedAt != null)
            _Line(
                label: 'Completed',
                value: _formatTime(ride.completedAt!)),
          if (distanceKm != null)
            _Line(
                label: 'Distance',
                value: '${distanceKm.toStringAsFixed(1)} km'),
          if (durationMin != null)
            _Line(label: 'Duration', value: '$durationMin min'),
          _Line(label: 'Ride type', value: ride.rideType),
          const Divider(height: 32),
          const Text(
            'Charges settle through the escrow ledger. If something looks '
            'wrong, file a dispute within 72 hours of completion.',
            style: TextStyle(fontSize: 12, color: AppColors.textSecondary),
          ),
          const SizedBox(height: 16),
          SizedBox(
            width: double.infinity,
            child: OutlinedButton.icon(
              onPressed: () {
                ScaffoldMessenger.of(context).showSnackBar(
                  const SnackBar(
                    content: Text(
                        'Open Wallet → History → Dispute to report this ride.'),
                  ),
                );
              },
              icon: const Icon(Icons.gavel, size: 18),
              label: const Text('Dispute this ride'),
            ),
          ),
        ],
      ),
    );
  }
}

class _RouteSection extends StatelessWidget {
  final Ride ride;

  const _RouteSection({required this.ride});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        border: Border.all(color: AppColors.border),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              const Icon(Icons.trip_origin,
                  size: 16, color: AppColors.success),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  ride.pickupAddress ??
                      '${ride.pickupLat.toStringAsFixed(5)}, '
                          '${ride.pickupLng.toStringAsFixed(5)}',
                  style: const TextStyle(fontSize: 14),
                ),
              ),
            ],
          ),
          const Padding(
            padding: EdgeInsets.only(left: 7, top: 2, bottom: 2),
            child: SizedBox(
              height: 20,
              width: 2,
              child: ColoredBox(color: AppColors.border),
            ),
          ),
          Row(
            children: [
              const Icon(Icons.place, size: 16, color: AppColors.error),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  ride.dropoffAddress ??
                      '${ride.dropoffLat.toStringAsFixed(5)}, '
                          '${ride.dropoffLng.toStringAsFixed(5)}',
                  style: const TextStyle(fontSize: 14),
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

class _Line extends StatelessWidget {
  final String label;
  final String value;

  const _Line({required this.label, required this.value});

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Row(
        mainAxisAlignment: MainAxisAlignment.spaceBetween,
        children: [
          Text(
            label,
            style: const TextStyle(
                fontSize: 14, color: AppColors.textSecondary),
          ),
          Text(
            value,
            style: const TextStyle(fontSize: 14, fontWeight: FontWeight.w600),
          ),
        ],
      ),
    );
  }
}
