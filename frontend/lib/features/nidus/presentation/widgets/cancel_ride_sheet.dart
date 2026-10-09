import 'package:flutter/material.dart';

import '../../../../core/theme/app_colors.dart';

const List<String> kRideCancellationReasons = <String>[
  'Wait time too long',
  'Driver is going the wrong way',
  'I need to reschedule',
  'Emergency',
  'Pickup location is incorrect',
  'Other',
];

/// Bottom sheet shown before a rider cancels. Surfaces the cancellation fee
/// up-front (dispute prevention — matches the backend's free-window policy)
/// and collects a structured reason that flows into the RideCancelled event.
class CancelRideSheet extends StatefulWidget {
  /// Estimated fee in currency units. Pass 0 during the free-cancel window;
  /// the bloc re-reads the authoritative value from the API response after
  /// the cancel actually executes.
  final double estimatedFee;
  final String currency;

  /// Remaining minutes of the free-cancel window, or null when the window
  /// has already elapsed / does not apply.
  final int? freeWindowMinutesLeft;
  final bool inProgress;

  final Future<void> Function(String reason) onConfirmed;

  const CancelRideSheet({
    super.key,
    required this.estimatedFee,
    this.currency = 'ETB',
    this.freeWindowMinutesLeft,
    this.inProgress = false,
    required this.onConfirmed,
  });

  /// Convenience presenter used by tracking/review pages.
  static Future<void> show(
    BuildContext context, {
    required double estimatedFee,
    String currency = 'ETB',
    int? freeWindowMinutesLeft,
    required Future<void> Function(String reason) onConfirmed,
  }) {
    return showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      backgroundColor: Colors.white,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
      ),
      builder: (sheetContext) => Padding(
        padding: EdgeInsets.only(
          bottom: MediaQuery.of(sheetContext).viewInsets.bottom,
        ),
        child: _CancelRideSheetContent(
          estimatedFee: estimatedFee,
          currency: currency,
          freeWindowMinutesLeft: freeWindowMinutesLeft,
          onConfirmed: onConfirmed,
        ),
      ),
    );
  }

  @override
  State<CancelRideSheet> createState() => _CancelRideSheetState();
}

class _CancelRideSheetState extends State<CancelRideSheet> {
  @override
  Widget build(BuildContext context) {
    return _CancelRideSheetContent(
      estimatedFee: widget.estimatedFee,
      currency: widget.currency,
      freeWindowMinutesLeft: widget.freeWindowMinutesLeft,
      inProgress: widget.inProgress,
      onConfirmed: widget.onConfirmed,
    );
  }
}

class _CancelRideSheetContent extends StatefulWidget {
  final double estimatedFee;
  final String currency;
  final int? freeWindowMinutesLeft;
  final bool inProgress;
  final Future<void> Function(String reason) onConfirmed;

  const _CancelRideSheetContent({
    required this.estimatedFee,
    required this.currency,
    this.freeWindowMinutesLeft,
    this.inProgress = false,
    required this.onConfirmed,
  });

  @override
  State<_CancelRideSheetContent> createState() =>
      _CancelRideSheetContentState();
}

class _CancelRideSheetContentState extends State<_CancelRideSheetContent> {
  String? _selectedReason;
  bool _submitting = false;

  bool get _busy => _submitting || widget.inProgress;

  String get _feeSummary {
    if (widget.estimatedFee <= 0) {
      final left = widget.freeWindowMinutesLeft;
      return left != null && left > 0
          ? 'Free cancellation — $left min left in your grace window.'
          : 'No cancellation fee applies right now.';
    }
    return 'A ${widget.currency} ${widget.estimatedFee.toStringAsFixed(2)} '
        'cancellation fee may apply.';
  }

  Future<void> _confirm() async {
    final reason = _selectedReason ?? 'Other';
    setState(() => _submitting = true);
    try {
      await widget.onConfirmed(reason);
      if (mounted) Navigator.of(context).pop();
    } finally {
      if (mounted) setState(() => _submitting = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(24, 16, 24, 24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Center(
              child: Container(
                width: 40,
                height: 4,
                decoration: BoxDecoration(
                  color: AppColors.border,
                  borderRadius: BorderRadius.circular(2),
                ),
              ),
            ),
            const SizedBox(height: 16),
            Row(
              children: [
                const Icon(Icons.close_rounded, color: AppColors.error),
                const SizedBox(width: 8),
                const Text(
                  'Cancel this ride?',
                  style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold),
                ),
              ],
            ),
            const SizedBox(height: 8),
            Text(
              _feeSummary,
              style: TextStyle(
                fontSize: 13,
                color: widget.estimatedFee > 0
                    ? AppColors.warning
                    : AppColors.textSecondary,
              ),
            ),
            const SizedBox(height: 16),
            Text(
              'Help us improve — why are you cancelling?',
              style: TextStyle(
                fontSize: 14,
                fontWeight: FontWeight.w600,
                color: AppColors.textPrimary,
              ).copyWith(),
            ),
            const SizedBox(height: 8),
            Flexible(
              child: SingleChildScrollView(
                child: Column(
                  children: kRideCancellationReasons.map((reason) {
                    final selected = _selectedReason == reason;
                    return RadioListTile<String>(
                      dense: true,
                      contentPadding: EdgeInsets.zero,
                      activeColor: AppColors.primary,
                      value: reason,
                      groupValue: _selectedReason,
                      title: Text(reason,
                          style: const TextStyle(fontSize: 14)),
                      onChanged: _busy
                          ? null
                          : (value) =>
                              setState(() => _selectedReason = value),
                    );
                  }).toList(),
                ),
              ),
            ),
            const SizedBox(height: 16),
            Row(
              children: [
                Expanded(
                  child: OutlinedButton(
                    onPressed:
                        _busy ? null : () => Navigator.of(context).pop(),
                    child: const Text('Keep ride'),
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: ElevatedButton.icon(
                    style: ElevatedButton.styleFrom(
                      backgroundColor: AppColors.error,
                      foregroundColor: Colors.white,
                    ),
                    onPressed: _busy ? null : _confirm,
                    icon: _submitting
                        ? const SizedBox(
                            width: 16,
                            height: 16,
                            child: CircularProgressIndicator(
                              strokeWidth: 2,
                              color: Colors.white,
                            ),
                          )
                        : const Icon(Icons.cancel_outlined, size: 18),
                    label: Text(
                      widget.estimatedFee > 0
                          ? 'Cancel (${widget.currency} ${widget.estimatedFee.toStringAsFixed(2)})'
                          : 'Cancel ride',
                    ),
                  ),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}
