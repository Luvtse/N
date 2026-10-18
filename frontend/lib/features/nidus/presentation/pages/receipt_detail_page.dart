import 'package:flutter/material.dart';

import '../../../../core/di/injection.dart';
import '../../../../core/theme/app_colors.dart';
import '../../data/repositories/phase2_money_repository.dart';
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
    // §3.4: when the backend sends fare_breakdown we render every line it
    // carries and omit the ones it does not. When absent (legacy rides or
    // pre-§3.4 records) we show only the known components and label the
    // remainder honestly instead of inventing a breakdown that could
    // contradict the escrowed charge.
    final breakdown = ride.fareBreakdown;
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
                if (ride.isCashRide) ...[
                  const SizedBox(height: 6),
                  Text(
                    ride.cashCollectedAt != null
                        ? 'Paid in cash • collected ${_formatTime(ride.cashCollectedAt!)}'
                        : 'Cash payment pending driver confirmation',
                    style: const TextStyle(
                        fontSize: 12,
                        fontWeight: FontWeight.w600,
                        color: AppColors.accentDark),
                  ),
                ],
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

          // ---- §3.4 Itemized fare breakdown -------------------------------
          if (breakdown != null) ...[
            const Divider(height: 32),
            const Text(
              'Fare breakdown',
              style: TextStyle(fontSize: 15, fontWeight: FontWeight.w700),
            ),
            const SizedBox(height: 8),
            if (breakdown.base != null)
              _Line(label: 'Base fare', value: _formatMoney(breakdown.base!)),
            if (breakdown.distance != null)
              _Line(
                  label: 'Distance',
                  value: _formatMoney(breakdown.distance!)),
            if (breakdown.time != null)
              _Line(label: 'Time', value: _formatMoney(breakdown.time!)),
            if (breakdown.surge != null && breakdown.surge! > 0)
              _Line(
                  label: 'Surge', value: _formatMoney(breakdown.surge!)),
            if (breakdown.serviceFee != null)
              _Line(
                  label: 'Service fee',
                  value: _formatMoney(breakdown.serviceFee!)),
            if (breakdown.promoDiscount != null &&
                breakdown.promoDiscount! > 0)
              _Line(
                label: 'Promo discount${ride.promoCode != null ? ' (${ride.promoCode})' : ''}',
                value: '-${_formatMoney(breakdown.promoDiscount!)}',
                positive: false,
              ),
            if (breakdown.tip != null && breakdown.tip! > 0)
              _Line(label: 'Tip', value: _formatMoney(breakdown.tip!)),
            const Divider(height: 24),
            _Line(
              label: 'Total',
              value: _formatMoney(fare),
              emphasize: true,
            ),
          ] else ...[
            if (ride.promoDiscount != null && ride.promoDiscount! > 0)
              _Line(
                label:
                    'Promo discount${ride.promoCode != null ? ' (${ride.promoCode})' : ''}',
                value: '-${_formatMoney(ride.promoDiscount!)}',
                positive: false,
              ),
            const Divider(height: 32),
            const Text(
              'This ride predates itemized receipts — only the total and the '
              'discount above are available. Charges settle through the '
              'escrow ledger.',
              style: TextStyle(fontSize: 12, color: AppColors.textSecondary),
            ),
          ],

          const SizedBox(height: 16),
          const Text(
            'If something looks wrong, file a dispute within 72 hours of '
            'completion.',
            style: TextStyle(fontSize: 12, color: AppColors.textSecondary),
          ),
          const SizedBox(height: 16),

          // ---- §3.4 actions ------------------------------------------------
          Row(
            children: [
              Expanded(
                child: OutlinedButton.icon(
                  onPressed: () => _downloadPdf(context),
                  icon: const Icon(Icons.picture_as_pdf, size: 18),
                  label: const Text('Download PDF'),
                ),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: OutlinedButton.icon(
                  onPressed: ride.status == 'completed'
                      ? () => _emailReceipt(context)
                      : null,
                  icon: const Icon(Icons.mail_outline, size: 18),
                  label: const Text('Email receipt'),
                ),
              ),
            ],
          ),
          const SizedBox(height: 12),
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

  /// Client-side PDF generation from the DTO (plan §3.4: "The PDF is
  /// generated client-side from the DTO"). The printable document lives in
  /// receipt_pdf_document.dart so this page stays presentational.
  void _downloadPdf(BuildContext context) {
    try {
      saveReceiptPdf(ride);
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Receipt saved.')),
      );
    } catch (e) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('Could not save the PDF: $e')),
      );
    }
  }

  Future<void> _emailReceipt(BuildContext context) async {
    final messenger = ScaffoldMessenger.of(context);
    final result =
        await getIt<Phase2MoneyRepository>().emailReceipt(ride.id);
    if (result.ok) {
      messenger.showSnackBar(
        const SnackBar(
          content: Text(
              'Receipt queued for delivery to your account email.'),
        ),
      );
    } else {
      messenger.showSnackBar(
        SnackBar(
          content: Text(result.message ??
              'Receipt email unavailable — try again later.'),
        ),
      );
    }
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

  /// Discounts render in the success color with an explicit minus sign so a
  /// negative line can never be mistaken for a charge.
  final bool positive;

  /// Total row gets heavier type.
  final bool emphasize;

  const _Line({
    required this.label,
    required this.value,
    this.positive = true,
    this.emphasize = false,
  });

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Row(
        mainAxisAlignment: MainAxisAlignment.spaceBetween,
        children: [
          Text(
            label,
            style: TextStyle(
              fontSize: 14,
              color: AppColors.textSecondary,
              fontWeight: emphasize ? FontWeight.w700 : FontWeight.w400,
            ),
          ),
          Text(
            value,
            style: TextStyle(
              fontSize: emphasize ? 16 : 14,
              fontWeight:
                  emphasize ? FontWeight.w800 : FontWeight.w600,
              color: positive ? null : AppColors.success,
            ),
          ),
        ],
      ),
    );
  }
}
