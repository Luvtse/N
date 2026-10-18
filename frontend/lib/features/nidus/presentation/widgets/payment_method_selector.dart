import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';

import '../../../../core/di/injection.dart';
import '../../../../core/theme/app_colors.dart';
import '../../../wallet/data/repositories/wallet_repository.dart';
import '../../../wallet/domain/entities/ledger_models.dart';
import '../../../wallet/presentation/bloc/wallet_bloc.dart';
import '../../../wallet/presentation/widgets/topup_sheet.dart';
import '../../data/repositories/phase2_money_repository.dart';
import '../bloc/ride_bloc.dart';

/// Payment rail picker for the booking flow (§3.1 rewrite).
///
/// The list is registry-driven: GET /nidus/payment-methods returns every
/// saved method (wallet rails, cards with tokenized metadata only — never a
/// PAN, cash) and each row maps onto RequestRide.paymentMethodId +
/// paymentMethodType ('wallet' | 'card' | 'cash'). When the registry has no
/// rows yet (fresh install, or FEATURE_CASH_PAYMENT off server-side), the
/// selector degrades to the known wallet rails from [PaymentProvider] so the
/// rider can still book; that fallback is labeled honestly in the UI.
///
/// Cash selection shows a confirmation ("paid to the driver at the end of
/// the trip") before it becomes the active choice, per §3.1.
class PaymentMethodSelector extends StatefulWidget {
  final String? selectedPaymentMethodId;
  final String? selectedPaymentMethodType;
  final void Function(String? id, String? type) onChanged;

  const PaymentMethodSelector({
    super.key,
    required this.selectedPaymentMethodId,
    required this.onChanged,
    this.selectedPaymentMethodType,
  });

  @override
  State<PaymentMethodSelector> createState() => _PaymentMethodSelectorState();
}

class _PaymentMethodSelectorState extends State<PaymentMethodSelector> {
  LedgerBalance? _balance;
  bool _loadingBalance = false;
  String? _balanceError;

  List<SavedPaymentMethod> _methods = const [];
  bool _loadingMethods = true;
  String? _methodsError;
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    _loadBalance();
    _loadMethods();
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

  Future<void> _loadMethods() async {
    setState(() {
      _loadingMethods = true;
      _methodsError = null;
    });
    final result = await getIt<Phase2MoneyRepository>().listPaymentMethods();
    if (!mounted) return;
    setState(() {
      _loadingMethods = false;
      if (result.ok) {
        _methods = result.methods;
        _methodsError = null;
      } else {
        _methods = const [];
        _methodsError = result.failureMessage ??
            'Payment methods unavailable.';
      }
    });
  }

  /// Selecting an existing registry row.
  void _select(SavedPaymentMethod m) {
    if (_busy) return;
    final alreadySelected = widget.selectedPaymentMethodId == m.id;
    if (alreadySelected) {
      // Tapping the active chip clears the explicit choice; the backend then
      // resolves the rider's default method.
      widget.onChanged(null, null);
      return;
    }
    if (m.isCash) {
      _confirmCashThenSelect(m);
      return;
    }
    widget.onChanged(m.id, m.type);
  }

  Future<void> _confirmCashThenSelect(SavedPaymentMethod m) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('Pay with cash'),
        content: const Text(
          'You will pay the fare in cash directly to the driver at the end '
          'of the trip. The driver confirms collection before the ride is '
          'closed out.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(dialogContext).pop(true),
            child: const Text('Use cash'),
          ),
        ],
      ),
    );
    if (confirmed == true && mounted) {
      widget.onChanged(m.id, m.type);
    }
  }

  /// Wallet-rail fallback rows when the registry is empty/unreachable.
  void _selectRail(PaymentProvider provider) {
    final selected =
        widget.selectedPaymentMethodId == provider.apiValue;
    widget.onChanged(selected ? null : provider.apiValue, 'wallet');
  }

  Future<void> _addCash() async {
    if (_busy) return;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('Add cash payments'),
        content: const Text(
          'Cash rides are paid to the driver at the end of the trip. Add '
          '"Cash" as a selectable payment option?',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(dialogContext).pop(true),
            child: const Text('Add cash'),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;
    setState(() => _busy = true);
    final result = await getIt<Phase2MoneyRepository>()
        .addPaymentMethod(type: 'cash');
    if (!mounted) return;
    setState(() => _busy = false);
    if (result.ok) {
      await _loadMethods();
      if (result.id != null && mounted) {
        widget.onChanged(result.id, 'cash');
      }
    } else {
      _showSnack(result.message ?? 'Could not add cash option.');
    }
  }

  Future<void> _setDefault(SavedPaymentMethod m) async {
    if (_busy) return;
    setState(() => _busy = true);
    final result = await getIt<Phase2MoneyRepository>()
        .setDefaultPaymentMethod(m.id);
    if (!mounted) return;
    setState(() => _busy = false);
    if (result.ok) {
      await _loadMethods();
    } else {
      _showSnack(result.message ?? 'Could not update default method.');
    }
  }

  Future<void> _remove(SavedPaymentMethod m) async {
    if (_busy) return;
    setState(() => _busy = true);
    final result =
        await getIt<Phase2MoneyRepository>().deletePaymentMethod(m.id);
    if (!mounted) return;
    setState(() => _busy = false);
    if (result.ok) {
      if (widget.selectedPaymentMethodId == m.id) {
        widget.onChanged(null, null);
      }
      await _loadMethods();
    } else {
      _showSnack(result.message ?? 'Could not remove payment method.');
    }
  }

  void _showSnack(String message) {
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text(message)),
    );
  }

  IconData _iconFor(SavedPaymentMethod m) {
    switch (m.type) {
      case 'cash':
        return Icons.payments_outlined;
      case 'card':
        return Icons.credit_card;
      case 'wallet':
      default:
        return Icons.account_balance_wallet;
    }
  }

  String _subtitleFor(SavedPaymentMethod m) {
    if (m.type == 'card') {
      final brand = m.brand ?? 'Card';
      final last4 = m.last4 != null ? '•••• ${m.last4}' : '';
      final expiry = (m.expMonth != null && m.expYear != null)
          ? ' · exp ${m.expMonth!.toString().padLeft(2, '0')}/${m.expYear}'
          : '';
      return '$brand $last4$expiry';
    }
    if (m.type == 'cash') return 'Paid to the driver at drop-off';
    return m.label;
  }

  @override
  Widget build(BuildContext context) {
    final available = _balance?.available.etb;
    final showRegistryFallback =
        !_loadingMethods && _methods.isEmpty && _methodsError == null;

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

        if (_loadingMethods)
          const Padding(
            padding: EdgeInsets.symmetric(vertical: 12),
            child: LinearProgressIndicator(minHeight: 2),
          )
        else if (_methodsError != null) ...[
          // Honest degraded state: the registry could not be reached. We do
          // NOT hide it behind fake data — wallet rails stay selectable and
          // a retry affordance is shown (Global Rule: unavailable + retry).
          Row(
            children: [
              const Icon(Icons.error_outline,
                  size: 16, color: AppColors.textSecondary),
              const SizedBox(width: 6),
              Expanded(
                child: Text(
                  '$_methodsError Using wallet rails only.',
                  style: const TextStyle(
                      fontSize: 12, color: AppColors.textSecondary),
                ),
              ),
              TextButton(
                onPressed: _loadMethods,
                child: const Text('Retry', style: TextStyle(fontSize: 12)),
              ),
            ],
          ),
          const SizedBox(height: 8),
        ],

        // Registry rows (when loaded).
        if (_methods.isNotEmpty) ...[
          ..._methods.map((m) {
            final selected = widget.selectedPaymentMethodId == m.id;
            return Padding(
              padding: const EdgeInsets.only(bottom: 8),
              child: InkWell(
                borderRadius: BorderRadius.circular(12),
                onTap: _busy ? null : () => _select(m),
                child: Container(
                  padding: const EdgeInsets.symmetric(
                      horizontal: 12, vertical: 10),
                  decoration: BoxDecoration(
                    borderRadius: BorderRadius.circular(12),
                    border: Border.all(
                      color: selected
                          ? AppColors.primary
                          : AppColors.border,
                      width: selected ? 2 : 1,
                    ),
                  ),
                  child: Row(
                    children: [
                      Icon(
                        selected
                            ? Icons.check_circle
                            : _iconFor(m),
                        size: 20,
                        color: selected
                            ? AppColors.primary
                            : AppColors.textSecondary,
                      ),
                      const SizedBox(width: 10),
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(
                              m.label,
                              style: const TextStyle(
                                  fontSize: 14,
                                  fontWeight: FontWeight.w600),
                            ),
                            Text(
                              _subtitleFor(m),
                              style: const TextStyle(
                                  fontSize: 12,
                                  color: AppColors.textSecondary),
                            ),
                          ],
                        ),
                      ),
                      if (m.isDefault)
                        const Chip(
                          visualDensity: VisualDensity.compact,
                          label: Text('Default',
                              style: TextStyle(fontSize: 10)),
                        )
                      else
                        IconButton(
                          tooltip: 'Set as default',
                          visualDensity: VisualDensity.compact,
                          icon: const Icon(Icons.star_border, size: 20),
                          onPressed:
                              _busy ? null : () => _setDefault(m),
                        ),
                      if (!m.isDefault)
                        IconButton(
                          tooltip: 'Remove',
                          visualDensity: VisualDensity.compact,
                          icon: const Icon(Icons.delete_outline, size: 20),
                          onPressed:
                              _busy ? null : () => _remove(m),
                        ),
                    ],
                  ),
                ),
              ),
            );
          }),
          Align(
            alignment: Alignment.centerLeft,
            child: TextButton.icon(
              onPressed: _busy ? null : _addCash,
              icon: const Icon(Icons.add, size: 18),
              label: const Text('Add cash option'),
            ),
          ),
        ] else if (!_loadingMethods && _methodsError == null) ...[
          // Registry reachable but empty (fresh account): fall back to the
          // known wallet rails, clearly labeled.
          if (showRegistryFallback)
            const Padding(
              padding: EdgeInsets.only(bottom: 8),
              child: Text(
                'No saved methods yet — choose a wallet rail below.',
                style: TextStyle(
                    fontSize: 12, color: AppColors.textSecondary),
              ),
            ),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: PaymentProvider.values.map((provider) {
              final selected =
                  widget.selectedPaymentMethodId == provider.apiValue;
              return ChoiceChip(
                selected: selected,
                avatar: Icon(
                  selected
                      ? Icons.check_circle
                      : Icons.account_balance_wallet,
                  size: 18,
                  color:
                      selected ? AppColors.primary : AppColors.textSecondary,
                ),
                onSelected: (_) => _selectRail(provider),
                label: Text(provider.label),
              );
            }).toList(),
          ),
          const SizedBox(height: 8),
          Align(
            alignment: Alignment.centerLeft,
            child: TextButton.icon(
              onPressed: _busy ? null : _addCash,
              icon: const Icon(Icons.payments_outlined, size: 18),
              label: const Text('Add cash option'),
            ),
          ),
        ],

        if (widget.selectedPaymentMethodId == null &&
            !_loadingMethods) ...[
          const SizedBox(height: 6),
          const Text(
            'No selection uses your default payment method.',
            style: TextStyle(fontSize: 12, color: AppColors.textSecondary),
          ),
        ],
        if (widget.selectedPaymentMethodType == 'cash' &&
            widget.selectedPaymentMethodId != null) ...[
          const SizedBox(height: 6),
          Row(
            children: const [
              Icon(Icons.payments_outlined,
                  size: 14, color: AppColors.textSecondary),
              SizedBox(width: 6),
              Expanded(
                child: Text(
                  'Cash ride: pay the driver at the end of the trip.',
                  style: TextStyle(
                      fontSize: 12, color: AppColors.textSecondary),
                ),
              ),
            ],
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

    void retryWith(String? rail, String? railType) {
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
        paymentMethodType: railType,
        promoCode: original.promoCode,
        scheduledAt: original.scheduledAt,
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
          paymentMethodType: original.paymentMethodType,
          promoCode: original.promoCode,
          scheduledAt: original.scheduledAt,
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
                    : () => retryWith(originalRequest!.paymentMethodId,
                        originalRequest!.paymentMethodType),
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
                  onPressed: () => retryWith(p.apiValue, 'wallet'),
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
