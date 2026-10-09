import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';

import '../../../../core/di/injection.dart';
import '../../../../core/theme/app_colors.dart';
import '../../../wallet/data/repositories/wallet_repository.dart';
import '../../../wallet/domain/entities/ledger_models.dart';
import '../../../wallet/presentation/bloc/wallet_bloc.dart';
import '../../../wallet/presentation/widgets/topup_sheet.dart';
import '../bloc/ride_bloc.dart';

/// Payment rail picker for the booking flow. Sets RequestRide.paymentMethodId
/// (previously never populated) to a provider apiValue ('telebirr' | 'chapa'
/// | 'mpesa'), which the backend maps onto its PaymentProvider enum and
/// applies server-side fallback priority when a rail declines.
class PaymentMethodSelector extends StatefulWidget {
  final String? selectedPaymentMethodId;
  final ValueChanged<String?> onChanged;

  const PaymentMethodSelector({
    super.key,
    required this.selectedPaymentMethodId,
    required this.onChanged,
  });

  @override
  State<PaymentMethodSelector> createState() => _PaymentMethodSelectorState();
}

class _PaymentMethodSelectorState extends State<PaymentMethodSelector> {
  LedgerBalance? _balance;
  bool _loadingBalance = false;
  String? _balanceError;

  @override
  void initState() {
    super.initState();
    _loadBalance();
  }

  Future<void> _loadBalance() async {
    setState(() {
      _loadingBalance = true;
      _balanceError = null;
    });
    try {
      final balance = await getIt<WalletRepository>().getBalance();
      if (!mounted) return;
      setState(() {
        _balance = balance;
        _loadingBalance = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _loadingBalance = false;
        _balanceError = 'Could not load wallet balance.';
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final available = _balance?.available.etb;

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: [
        Row(
          children: [
            const Text(
              'Pay with',
              style: TextStyle(fontSize: 15, fontWeight: FontWeight.w700),
            ),
            const Spacer(),
            if (_loadingBalance)
              const SizedBox(
                width: 14,
                height: 14,
                child: CircularProgressIndicator(strokeWidth: 2),
              )
            else if (available != null)
              TextButton.icon(
                onPressed: _loadBalance,
                icon: const Icon(Icons.refresh, size: 16),
                label: Text(
                  '${_balance!.currency} ${available.toStringAsFixed(2)} '
                  'available',
                  style: const TextStyle(fontSize: 12),
                ),
              )
            else if (_balanceError != null)
              TextButton(
                onPressed: _loadBalance,
                child: const Text('Retry', style: TextStyle(fontSize: 12)),
              ),
          ],
        ),
        const SizedBox(height: 4),
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: PaymentProvider.values.map((provider) {
            final selected = widget.selectedPaymentMethodId ==
                provider.apiValue;
            return ChoiceChip(
              selected: selected,
              avatar: Icon(
                selected ? Icons.check_circle : Icons.account_balance_wallet,
                size: 18,
                color: selected ? AppColors.primary : AppColors.textSecondary,
              ),
              onSelected: (_) => widget.onChanged(
                selected ? null : provider.apiValue,
              ),
              label: Text(provider.label),
            );
          }).toList(),
        ),
        if (widget.selectedPaymentMethodId == null) ...[
          const SizedBox(height: 6),
          const Text(
            'No selection uses the wallet default rail.',
            style: TextStyle(fontSize: 12, color: AppColors.textSecondary),
          ),
        ],
      ],
    );
  }
}

/// Recovery sheet rendered when RideBloc emits PaymentFailed (backend code
/// PAYMENT_FAILED / INVALID_PAYMENT). Offers retry, rail switch, and top-up
/// via the existing WalletBloc + TopupSheet plumbing — no new payment code.
class PaymentFailureSheet extends StatelessWidget {
  final String message;
  final String? errorCode;

  /// The original request, replayed on "Try again" / rail switch. Null when
  /// the failure surfaced without retained context (e.g. cold WS error) — the
  /// sheet then offers recovery actions instead of a blind retry.
  final RequestRide? originalRequest;

  const PaymentFailureSheet({
    super.key,
    required this.message,
    this.originalRequest,
    this.errorCode,
  });

  static Future<void> show(
    BuildContext context, {
    required String message,
    RequestRide? originalRequest,
    String? errorCode,
  }) {
    return showModalBottomSheet<void>(
      context: context,
      backgroundColor: Colors.white,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
      ),
      builder: (_) => Padding(
        padding: EdgeInsets.only(
          bottom: MediaQuery.of(context).viewInsets.bottom,
        ),
        child: PaymentFailureSheet(
          message: message,
          errorCode: errorCode,
          originalRequest: originalRequest,
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final rideBloc = context.read<RideBloc>();

    void retryWith(String? rail) {
      final original = originalRequest;
      if (original == null) return;
      Navigator.of(context).pop();
      rideBloc.add(RequestRide(
        pickupLat: original.pickupLat,
        pickupLng: original.pickupLng,
        dropoffLat: original.dropoffLat,
        dropoffLng: original.dropoffLng,
        pickupAddress: original.pickupAddress,
        dropoffAddress: original.dropoffAddress,
        rideType: original.rideType,
        paymentMethodId: rail,
      ));
    }

    Future<void> topUpFirst() async {
      // Capture the bloc before closing this sheet — the sheet's own
      // BuildContext dies with it and must not be used afterwards.
      final bloc = context.read<RideBloc>();
      final original = originalRequest;
      Navigator.of(context).pop();
      final request = await TopupSheet.show(context);
      if (request == null) return;
      // WalletBloc is a getIt factory (see core/di/injection.dart); a fresh
      // instance dispatches the top-up against the shared WalletRepository.
      final walletBloc = getIt<WalletBloc>();
      walletBloc.add(StartTopup(
        amount: request.amount,
        provider: request.provider,
      ));
      // Replay the original booking once the rails have had a moment to
      // settle the optimistic credit.
      if (original != null) {
        await Future<void>.delayed(const Duration(seconds: 2));
        bloc.add(RequestRide(
          pickupLat: original.pickupLat,
          pickupLng: original.pickupLng,
          dropoffLat: original.dropoffLat,
          dropoffLng: original.dropoffLng,
          pickupAddress: original.pickupAddress,
          dropoffAddress: original.dropoffAddress,
          rideType: original.rideType,
          paymentMethodId: original.paymentMethodId,
        ));
      }
    }

    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(24, 16, 24, 24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                const Icon(Icons.credit_card_off, color: AppColors.error),
                const SizedBox(width: 8),
                const Text(
                  'Payment failed',
                  style: TextStyle(
                      fontSize: 18, fontWeight: FontWeight.bold),
                ),
              ],
            ),
            const SizedBox(height: 8),
            Text(
              message.isEmpty
                  ? 'Your payment could not be processed.'
                  : message,
              style: const TextStyle(
                  fontSize: 13, color: AppColors.textSecondary),
            ),
            const SizedBox(height: 4),
            const Text(
              'You have NOT been charged twice — retries reuse the same '
              'payment reference.',
              style: TextStyle(fontSize: 12, color: AppColors.textSecondary),
            ),
            const SizedBox(height: 16),
            SizedBox(
              width: double.infinity,
              child: ElevatedButton.icon(
                onPressed: originalRequest == null
                    ? () => Navigator.of(context).pop()
                    : () => retryWith(originalRequest!.paymentMethodId),
                icon: const Icon(Icons.refresh),
                label: Text(originalRequest == null
                    ? 'Back to booking'
                    : 'Try again'),
              ),
            ),
            const SizedBox(height: 16),
            const Text(
              'Or pay with another method',
              style: TextStyle(fontWeight: FontWeight.w600, fontSize: 14),
            ),
            const SizedBox(height: 8),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: PaymentProvider.values
                  .where((p) => p.apiValue != originalRequest?.paymentMethodId)
                  .map((p) {
                return ActionChip(
                  avatar: const Icon(Icons.swap_horiz, size: 16),
                  label: Text(p.label),
                  onPressed: () => retryWith(p.apiValue),
                );
              }).toList(),
            ),
            const SizedBox(height: 16),
            OutlinedButton.icon(
              onPressed: topUpFirst,
              icon: const Icon(Icons.add_money_online),
              label: const Text('Top up wallet first'),
            ),
          ],
        ),
      ),
    );
  }
}
