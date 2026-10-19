import 'dart:async';

import 'package:flutter/material.dart';
import 'package:get_it/get_it.dart';

import '../../../../core/di/injection.dart';
import '../../../../core/network/api_client.dart';
import '../../../../core/theme/app_colors.dart';
import '../../../../l10n/l10n.dart';
import '../../data/repositories/ride_repository.dart';

/// §4.3: the display strings are localized (Amharic/Oromo included), but the
/// value sent to the backend stays a stable English code — cancel_reason is
/// persisted verbatim and read by analytics/support tooling, so localizing
/// the wire value would corrupt records. The list is built in build() because
/// AppLocalizations requires context.
const List<String> kRideCancellationReasonCodes = <String>[
  'wait_time_too_long',
  'driver_wrong_way',
  'need_reschedule',
  'emergency',
  'pickup_incorrect',
  'other',
];

/// §2.5 — Authoritative cancellation-fee quote sheet.
///
/// The fee, the reason code, the free-window countdown and the itemized
/// breakdown all come from GET /rides/{rideId}/cancellation-quote, which the
/// backend computes with the *same* policy service that charges the fee on
/// CancelRide — so the number shown here is exactly the number the rider pays.
/// There are no client-side fee heuristics in this file (the old
/// `feeEstimate = 25.0` / `freeWindowMinutesLeft = 5` guesses are deleted).
///
/// Degradation contract: if the quote fetch fails, the sheet shows
/// "Fee will be confirmed on cancel" and still lets the rider proceed. It
/// never invents a number and never blanks the screen.
class CancelRideSheet extends StatefulWidget {
  /// Ride the quote is fetched for. Required.
  final String rideId;

  /// Invoked with the selected structured reason once the rider confirms.
  /// The caller dispatches the actual cancel through the bloc; the sheet
  /// closes only after this future completes without throwing.
  final Future<void> Function(String reason) onConfirmed;

  const CancelRideSheet({
    super.key,
    required this.rideId,
    required this.onConfirmed,
  });

  /// Convenience presenter used by the tracking page.
  static Future<void> show(
    BuildContext context, {
    required String rideId,
    required Future<void> Function(String reason) onConfirmed,
  }) {
    return showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      // §4.4 dark-mode token: sheet surface follows the theme instead of a
      // hardcoded white that clips invisible text in dark mode.
      backgroundColor: AppColors.surface,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
      ),
      builder: (sheetContext) => Padding(
        padding: EdgeInsets.only(
          bottom: MediaQuery.of(sheetContext).viewInsets.bottom,
        ),
        child: CancelRideSheet(
          rideId: rideId,
          onConfirmed: onConfirmed,
        ),
      ),
    );
  }

  @override
  State<CancelRideSheet> createState() => _CancelRideSheetState();
}

enum _QuoteStatus { loading, ready, failed }

class _CancelRideSheetState extends State<CancelRideSheet> {
  final RideRepository _repository = getIt<RideRepository>();

  _QuoteStatus _status = _QuoteStatus.loading;
  CancellationQuote? _quote;
  String? _failureMessage;

  String? _selectedReason;
  bool _submitting = false;

  /// Seconds left on the free window, ticking locally from the value the
  /// backend returned so open/close cycles don't hammer the quote endpoint
  /// (which itself caches per ride for 30 s server-side).
  int? _secondsRemaining;
  Timer? _ticker;

  @override
  void initState() {
    super.initState();
    _fetchQuote();
  }

  @override
  void dispose() {
    _ticker?.cancel();
    super.dispose();
  }

  Future<void> _fetchQuote() async {
    setState(() {
      _status = _QuoteStatus.loading;
      _failureMessage = null;
    });
    try {
      final quote = await _repository.getCancellationQuote(widget.rideId);
      if (!mounted) return;
      setState(() {
        _quote = quote;
        _status = _QuoteStatus.ready;
        _secondsRemaining = quote.freeWindowSecondsRemaining;
      });
      _startTickerIfFree();
    } on ApiException catch (e) {
      if (!mounted) return;
      // RIDE_NOT_CANCELLABLE means the ride already ended between the tap
      // and the fetch — say so plainly instead of showing a stale quote.
      setState(() {
        _status = _QuoteStatus.failed;
        _failureMessage = e.errorCode == 'RIDE_NOT_CANCELLABLE'
            ? 'This ride can no longer be cancelled.'
            : 'Fee will be confirmed on cancel.';
      });
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _status = _QuoteStatus.failed;
        _failureMessage = 'Fee will be confirmed on cancel.';
      });
    }
  }

  void _startTickerIfFree() {
    _ticker?.cancel();
    final q = _quote;
    if (q == null || !q.isFree || (_secondsRemaining ?? 0) <= 0) return;
    _ticker = Timer.periodic(const Duration(seconds: 1), (timer) {
      if (!mounted) {
        timer.cancel();
        return;
      }
      setState(() {
        _secondsRemaining = (_secondsRemaining ?? 1) - 1;
      });
      if ((_secondsRemaining ?? 0) <= 0) {
        timer.cancel();
        // The window just closed while the sheet was open — re-fetch once so
        // the displayed fee becomes authoritative again instead of guessing
        // what the new fee is.
        _fetchQuote();
      }
    });
  }

  Future<void> _confirm() async {
    final reason = _selectedReason ?? 'other';
    setState(() => _submitting = true);
    try {
      await widget.onConfirmed(reason);
      if (mounted) Navigator.of(context).pop();
    } finally {
      if (mounted) setState(() => _submitting = false);
    }
  }

  /// §4.3: maps the stable wire code to its localized display label.
  String _reasonLabel(AppLocalizations l10n, String code) {
    switch (code) {
      case 'wait_time_too_long':
        return l10n.cancelReasonWaitTooLong;
      case 'driver_wrong_way':
        return l10n.cancelReasonDriverWrongWay;
      case 'need_reschedule':
        return l10n.cancelReasonReschedule;
      case 'emergency':
        return l10n.cancelReasonEmergency;
      case 'pickup_incorrect':
        return l10n.cancelReasonPickupIncorrect;
      default:
        return l10n.cancelReasonOther;
    }
  }

  String get _reasonCopy {
    switch (_quote?.reasonCode) {
      case 'NO_DRIVER_YET':
        return 'No driver has been matched yet — cancelling is free.';
      case 'FREE_WINDOW':
        return 'You are inside the free-cancel window.';
      case 'DRIVER_ARRIVED':
        return 'The free window has passed, so a late-cancellation fee applies.';
      case 'TRIP_STARTED':
        return 'Your trip has started. Cancelling now charges the accrued '
            'fare plus a late-cancellation fee.';
      default:
        return '';
    }
  }

  String _formatCountdown(int seconds) {
    final m = seconds ~/ 60;
    final s = seconds % 60;
    return '$m:${s.toString().padLeft(2, '0')}';
  }

  @override
  Widget build(BuildContext context) {
    final quote = _quote;
    final showFeeLine = _status == _QuoteStatus.ready && quote != null;

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
                Text(
                  context.l10n.cancelSheetTitle,
                  style: const TextStyle(
                      fontSize: 18, fontWeight: FontWeight.bold),
                ),
              ],
            ),
            const SizedBox(height: 8),
            _buildFeeSection(showFeeLine, quote),
            const SizedBox(height: 16),
            Text(
              context.l10n.cancelSheetReasonPrompt,
              style: TextStyle(
                fontSize: 14,
                fontWeight: FontWeight.w600,
                color: AppColors.textPrimary,
              ),
            ),
            const SizedBox(height: 8),
            Flexible(
              child: SingleChildScrollView(
                child: Column(
                  children: kRideCancellationReasonCodes.map((code) {
                    // §4.3: stable wire code + localized display label.
                    final reason = code;
                    final selected = _selectedReason == reason;
                    // §4.4: announce selection state for each reason so a
                    // screen-reader user hears which option is checked.
                    return Semantics(
                      selected: selected,
                      checked: selected,
                      child: RadioListTile<String>(
                        dense: true,
                        contentPadding: EdgeInsets.zero,
                        activeColor: AppColors.primary,
                        value: reason,
                        groupValue: _selectedReason,
                        title: Text(
                          _reasonLabel(context.l10n, code),
                          style: const TextStyle(fontSize: 14),
                        ),
                        onChanged: _submitting
                            ? null
                            : (value) =>
                                setState(() => _selectedReason = value),
                      ),
                    );
                  }).toList(),
                ),
              ),
            ),
            const SizedBox(height: 16),
            Row(
              children: [
                Expanded(
                  // §4.4 accessibility: announced as a keep-ride action, not
                  // just the visible label, so TalkBack/VoiceOver users get
                  // the consequence of the button before activating it.
                  child: Semantics(
                    button: true,
                    label: context.l10n.cancelSheetDismissSemantics,
                    child: OutlinedButton(
                      onPressed: _submitting
                          ? null
                          : () => Navigator.of(context).pop(),
                      child: Text(context.l10n.cancelSheetDismiss),
                    ),
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Semantics(
                    button: true,
                    destructive: true,
                    label: context.l10n.cancelSheetConfirm +
                        _feeLabelForSemantics(context, showFeeLine, quote),
                    child: ElevatedButton.icon(
                      style: ElevatedButton.styleFrom(
                        backgroundColor: AppColors.error,
                        foregroundColor:
                            Theme.of(context).colorScheme.onError,
                      ),
                      onPressed: _submitting ? null : _confirm,
                      icon: _submitting
                          ? const SizedBox(
                              width: 16,
                              height: 16,
                              child: CircularProgressIndicator(
                                strokeWidth: 2,
                                color: AppColors.textOnDark,
                              ),
                            )
                          : const Icon(Icons.cancel_outlined, size: 18),
                      label: Text(_confirmLabel(context, showFeeLine, quote)),
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

  String _confirmLabel(
      BuildContext context, bool showFeeLine, CancellationQuote? quote) {
    if (showFeeLine && quote != null && quote.fee > 0) {
      return context.l10n.cancelSheetConfirmWithFee(
          amount: quote.fee.toStringAsFixed(2), currency: quote.currency);
    }
    return context.l10n.cancelSheetConfirm;
  }

  /// §4.4: fee suffix for the cancel button's Semantics label. Mirrors
  /// [_confirmLabel] so screen-reader users hear the same authoritative fee
  /// sighted users see; when no quote is available it says exactly that —
  /// never a fabricated number.
  String _feeLabelForSemantics(
      BuildContext context, bool showFeeLine, CancellationQuote? quote) {
    if (showFeeLine && quote != null && quote.fee > 0) {
      return ' — a ${quote.fee.toStringAsFixed(2)} ${quote.currency} '
          'cancellation fee will be charged';
    }
    if (showFeeLine && quote != null && quote.fee == 0) {
      return context.l10n.cancelSheetSemanticsFeeSuffixFree;
    }
    return context.l10n.cancelSheetSemanticsFeeSuffixUnknown;
  }

  Widget _buildFeeSection(bool showFeeLine, CancellationQuote? quote) {
    if (_status == _QuoteStatus.loading) {
      return Row(
        children: [
          const SizedBox(
            width: 14,
            height: 14,
            child: CircularProgressIndicator(strokeWidth: 2),
          ),
          const SizedBox(width: 8),
          Text(
            context.l10n.cancelSheetFeeLoading,
            style: const TextStyle(fontSize: 13, color: AppColors.textSecondary),
          ),
        ],
      );
    }

    if (_status == _QuoteStatus.failed) {
      return Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            _failureMessage ?? context.l10n.cancelSheetFeeUnknown,
            style: const TextStyle(
              fontSize: 13,
              color: AppColors.warning,
            ),
          ),
          const SizedBox(height: 8),
          Align(
            alignment: Alignment.centerLeft,
            child: TextButton.icon(
              onPressed: _fetchQuote,
              icon: const Icon(Icons.refresh, size: 18),
              label: Text(context.l10n.commonRetry),
            ),
          ),
        ],
      );
    }

    // Ready state — render the authoritative values verbatim.
    final q = quote!;
    final secs = _secondsRemaining ?? q.freeWindowSecondsRemaining;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          q.isFree
              ? context.l10n.cancelSheetNoFee
              : context.l10n.cancelSheetCostNow(
                  amount: q.fee.toStringAsFixed(2), currency: q.currency),
          style: TextStyle(
            fontSize: 13,
            fontWeight: FontWeight.w600,
            color: q.isFree ? AppColors.textSecondary : AppColors.warning,
          ),
        ),
        if (_reasonCopy.isNotEmpty) ...[
          const SizedBox(height: 4),
          Text(
            _reasonCopy,
            style: const TextStyle(
              fontSize: 13,
              color: AppColors.textSecondary,
            ),
          ),
        ],
        if (q.isFree && secs > 0) ...[
          const SizedBox(height: 4),
          Row(
            children: [
              const Icon(Icons.timer_outlined,
                  size: 16, color: AppColors.success),
              const SizedBox(width: 6),
              Text(
                context.l10n.cancelSheetFreeWindowEnds(_formatCountdown(secs)),
                style: const TextStyle(
                  fontSize: 13,
                  color: AppColors.success,
                  fontWeight: FontWeight.w600,
                ),
              ),
            ],
          ),
        ],
        if (q.breakdown.isNotEmpty) ...[
          const SizedBox(height: 12),
          ...q.breakdown.map(
            (item) => Padding(
              padding: const EdgeInsets.symmetric(vertical: 2),
              child: Row(
                mainAxisAlignment: MainAxisAlignment.spaceBetween,
                children: [
                  Expanded(
                    child: Text(
                      item.label,
                      style: const TextStyle(
                        fontSize: 13,
                        color: AppColors.textSecondary,
                      ),
                    ),
                  ),
                  Text(
                    '${item.currency} ${item.amount.toStringAsFixed(2)}',
                    style: const TextStyle(
                      fontSize: 13,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                ],
              ),
            ),
          ),
          const Divider(height: 20),
          Row(
            mainAxisAlignment: MainAxisAlignment.spaceBetween,
            children: [
              Text(
                context.l10n.cancelSheetTotalLabel,
                style: const TextStyle(fontSize: 13, fontWeight: FontWeight.w700),
              ),
              Text(
                '${q.currency} ${q.fee.toStringAsFixed(2)}',
                style: const TextStyle(
                  fontSize: 13,
                  fontWeight: FontWeight.w700,
                  color: AppColors.error,
                ),
              ),
            ],
          ),
        ],
      ],
    );
  }
}
