import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../../../core/theme/app_colors.dart';
import '../../data/repositories/wallet_repository.dart';
import '../../domain/entities/ledger_models.dart';

/// Bottom sheet collecting an amount + rail choice for a wallet top-up
/// (Phase H Step 1 / Phase E Step 3). Returns a [StartTopupRequest] when the
/// user confirms, or null when dismissed.
///
/// The actual capture is asynchronous server-side; this sheet only gathers
/// intent. Provider SDKs / checkout webviews are launched by the caller after
/// the ledger has accepted the request (optimistic credit first).
class TopupSheet extends StatefulWidget {
  const TopupSheet({super.key});

  static Future<StartTopupRequest?> show(BuildContext context) {
    return showModalBottomSheet<StartTopupRequest>(
      context: context,
      isScrollControlled: true,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(20)),
      ),
      builder: (_) => Padding(
        padding:
            EdgeInsets.only(bottom: MediaQuery.of(context).viewInsets.bottom),
        child: const TopupSheet(),
      ),
    );
  }

  @override
  State<TopupSheet> createState() => _TopupSheetState();
}

class _TopupSheetState extends State<TopupSheet> {
  static const List<int> _presetCents = [
    5000, // ETB 50
    10000, // ETB 100
    25000, // ETB 250
    50000, // ETB 500
    100000, // ETB 1000
  ];

  final TextEditingController _amountController = TextEditingController();
  PaymentProvider _provider = PaymentProvider.telebirr;
  String? _amountError;

  @override
  void dispose() {
    _amountController.dispose();
    super.dispose();
  }

  LedgerMoney? _validateAmount() {
    final raw = _amountController.text.trim();
    final value = double.tryParse(raw);
    if (value == null || value <= 0) {
      setState(() => _amountError = 'Enter a valid positive amount');
      return null;
    }
    final money = LedgerMoney((value * 100).round());
    if (money.cents < 100) {
      // Backend enforces a 1 ETB minimum on top-ups; mirror it client-side.
      setState(() => _amountError = 'Minimum top-up is ETB 1.00');
      return null;
    }
    if (money.cents > 10000000) {
      setState(() => _amountError = 'Maximum top-up is ETB 100,000.00');
      return null;
    }
    setState(() => _amountError = null);
    return money;
  }

  @override
  Widget build(BuildContext context) {
    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text(
              'Add Money',
              style: TextStyle(fontSize: 20, fontWeight: FontWeight.bold),
            ),
            const SizedBox(height: 4),
            Text(
              'Your balance is credited instantly; the payment is confirmed '
              'by ${_provider.label} in the background.',
              style: TextStyle(fontSize: 12, color: Colors.grey[600]),
            ),
            const SizedBox(height: 16),

            // Amount input
            TextField(
              controller: _amountController,
              keyboardType:
                  const TextInputType.numberWithOptions(decimal: true),
              inputFormatters: [
                FilteringTextInputFormatter.allow(RegExp(r'^\d*\.?\d{0,2}')),
              ],
              decoration: InputDecoration(
                labelText: 'Amount (ETB)',
                prefixText: 'ETB ',
                errorText: _amountError,
                border: const OutlineInputBorder(),
              ),
            ),
            const SizedBox(height: 12),

            // Preset chips
            Wrap(
              spacing: 8,
              children: _presetCents.map((cents) {
                return ActionChip(
                  label: Text('ETB ${(cents / 100).toStringAsFixed(0)}'),
                  onPressed: () {
                    _amountController.text =
                        (cents / 100).toStringAsFixed(2);
                    setState(() {});
                  },
                );
              }).toList(),
            ),
            const SizedBox(height: 20),

            // Rail selection (Phase E providers)
            const Text('Pay with',
                style: TextStyle(fontWeight: FontWeight.w600)),
            const SizedBox(height: 8),
            ...PaymentProvider.values.map((p) {
              return RadioListTile<PaymentProvider>(
                dense: true,
                contentPadding: EdgeInsets.zero,
                title: Text(p.label),
                subtitle: Text(p.description,
                    style: const TextStyle(fontSize: 12)),
                value: p,
                groupValue: _provider,
                onChanged: (v) =>
                    setState(() => _provider = v ?? _provider),
              );
            }),
            const SizedBox(height: 12),

            SizedBox(
              width: double.infinity,
              height: 48,
              child: ElevatedButton(
                style: ElevatedButton.styleFrom(
                  backgroundColor: AppColors.primary,
                  foregroundColor: Colors.white,
                  shape: RoundedRectangleBorder(
                      borderRadius: BorderRadius.circular(12)),
                ),
                onPressed: () {
                  final amount = _validateAmount();
                  if (amount == null) return;
                  Navigator.of(context).pop(
                    StartTopupRequest(amount: amount, provider: _provider),
                  );
                },
                child: const Text(
                  'Continue',
                  style: TextStyle(fontSize: 16, fontWeight: FontWeight.w600),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// Value object returned by [TopupSheet.show].
class StartTopupRequest {
  final LedgerMoney amount;
  final PaymentProvider provider;

  const StartTopupRequest({required this.amount, required this.provider});
}
