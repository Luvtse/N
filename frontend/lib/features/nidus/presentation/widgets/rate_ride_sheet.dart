import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';

import '../../../../core/theme/app_colors.dart';
import '../../data/repositories/ride_repository.dart';
import '../bloc/ride_bloc.dart';

/// Post-trip rating sheet. Dispatches the existing [RateRide] event, which
/// routes through RideRepository.rateRideWithResult so coded backend
/// failures (ALREADY_RATED / TIP_TOO_LARGE / TIP_SETTLEMENT_FAILED) get
/// precise copy instead of a generic error toast.
class RateRideSheet extends StatefulWidget {
  final String rideId;
  final double fare;
  final String currency;

  const RateRideSheet({
    super.key,
    required this.rideId,
    required this.fare,
    this.currency = 'ETB',
  });

  static Future<void> show(
    BuildContext context, {
    required String rideId,
    required double fare,
    String currency = 'ETB',
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
        child: RateRideSheet(
          rideId: rideId,
          fare: fare,
          currency: currency,
        ),
      ),
    );
  }

  @override
  State<RateRideSheet> createState() => _RateRideSheetState();
}

class _RateRideSheetState extends State<RateRideSheet> {
  int _rating = 0;
  double? _tip;
  final TextEditingController _reviewController = TextEditingController();
  bool _submitting = false;
  String? _error;

  static const List<double> _tipPresets = <double>[0, 10, 20, 30];

  @override
  void dispose() {
    _reviewController.dispose();
    super.dispose();
  }

  String get _driverFirstName {
    // Driver name isn't carried in RideCompleted's payload beyond the Ride
    // model; fall back to a friendly generic when unavailable.
    return 'your driver';
  }

  Future<void> _submit() async {
    if (_rating == 0) {
      setState(() => _error = 'Please pick a star rating first.');
      return;
    }
    final tip = _tip ?? 0;
    if (tip > widget.fare) {
      setState(() =>
          _error = 'Tip cannot exceed the ${widget.currency} '
              '${widget.fare.toStringAsFixed(2)} fare.');
      return;
    }

    setState(() {
      _submitting = true;
      _error = null;
    });

    final bloc = context.read<RideBloc>();
    bloc.add(RateRide(
      rideId: widget.rideId,
      rating: _rating,
      review: _reviewController.text.trim().isEmpty
          ? null
          : _reviewController.text.trim(),
      tip: tip > 0 ? tip : null,
    ));

    try {
      await Future.any<void>(<Future<void>>[
        bloc.stream
            .first((state) =>
                state is RideRated && state.rideId == widget.rideId ||
                state is RideError)
            .then<void>((state) {
          if (state is RideError) {
            throw StateError(state.message);
          }
        }),
        Future<void>.delayed(const Duration(seconds: 15))
            .then<void>((_) => throw TimeoutException('Rating timed out')),
      ]);

      if (!mounted) return;
      Navigator.of(context).pop();
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text(tip > 0
              ? 'Thanks! Your ${widget.currency} ${tip.toStringAsFixed(2)} '
                  'tip was sent.'
              : 'Thanks for the feedback!'),
          backgroundColor: AppColors.success,
        ),
      );
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _submitting = false;
        _error = e is TimeoutException
            ? 'Rating timed out — please check your connection and retry.'
            : e.toString();
      });
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
            Text(
              'How was your trip with $_driverFirstName?',
              style: const TextStyle(fontSize: 18, fontWeight: FontWeight.bold),
            ),
            const SizedBox(height: 4),
            Text(
              '${widget.currency} ${widget.fare.toStringAsFixed(2)} • '
              'Ride ${widget.rideId.substring(0, widget.rideId.length >= 8 ? 8 : widget.rideId.length)}',
              style: const TextStyle(
                fontSize: 13,
                color: AppColors.textSecondary,
              ),
            ),
            const SizedBox(height: 16),
            Row(
              mainAxisAlignment: MainAxisAlignment.center,
              children: List<int>.generate(5, (i) => i + 1).map((star) {
                return IconButton(
                  onPressed: () => setState(() {
                    _rating = star;
                    _error = null;
                  }),
                  icon: Icon(
                    star <= _rating ? Icons.star_rounded : Icons.star_outline,
                    size: 40,
                    color: star <= _rating
                        ? AppColors.accent
                        : AppColors.textSecondary,
                  ),
                  iconSize: 40,
                  splashRadius: 28,
                  tooltip: '$star star${star == 1 ? '' : 's'}',
                );
              }).toList(),
            ),
            const SizedBox(height: 8),
            if (_rating > 0 && _rating >= 4) ...[
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: <String>[
                  'Great music',
                  'Clean car',
                  'Safe driving',
                  'Friendly',
                ].map((chip) {
                  return ActionChip(
                    label: Text(chip, style: const TextStyle(fontSize: 12)),
                    onPressed: () {
                      final current = _reviewController.text.trim();
                      _reviewController.text = current.isEmpty
                          ? chip
                          : '$current, $chip';
                    },
                  );
                }).toList(),
              ),
              const SizedBox(height: 12),
            ],
            TextField(
              controller: _reviewController,
              maxLines: 2,
              maxLength: 200,
              decoration: InputDecoration(
                hintText: 'Anything else? (optional)',
                border: OutlineInputBorder(
                  borderRadius: BorderRadius.circular(12),
                ),
                contentPadding: const EdgeInsets.symmetric(
                  horizontal: 12,
                  vertical: 10,
                ),
              ),
            ),
            const SizedBox(height: 8),
            const Text(
              'Add a tip',
              style: TextStyle(fontWeight: FontWeight.w600, fontSize: 14),
            ),
            const SizedBox(height: 8),
            Wrap(
              spacing: 8,
              children: _tipPresets.map((tip) {
                final selected = (_tip ?? 0) == tip;
                return ChoiceChip(
                  selected: selected,
                  onSelected: (_) => setState(() {
                    _tip = tip;
                    _error = null;
                  }),
                  label: Text(
                    tip == 0
                        ? 'No tip'
                        : '${widget.currency} ${tip.toStringAsFixed(0)}',
                  ),
                );
              }).toList(),
            ),
            if (_error != null) ...[
              const SizedBox(height: 12),
              Text(
                _error!,
                style: const TextStyle(color: AppColors.error, fontSize: 13),
              ),
            ],
            const SizedBox(height: 16),
            SizedBox(
              width: double.infinity,
              child: ElevatedButton(
                onPressed: _submitting ? null : _submit,
                style: ElevatedButton.styleFrom(
                  backgroundColor: AppColors.primary,
                  foregroundColor: Colors.white,
                  padding: const EdgeInsets.symmetric(vertical: 14),
                ),
                child: _submitting
                    ? const SizedBox(
                        width: 20,
                        height: 20,
                        child: CircularProgressIndicator(
                          strokeWidth: 2,
                          color: Colors.white,
                        ),
                      )
                    : const Text('Submit rating'),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
