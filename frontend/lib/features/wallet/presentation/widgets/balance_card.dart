import 'package:flutter/material.dart';

import '../../../../core/theme/app_colors.dart';
import '../../domain/entities/ledger_models.dart';
import 'transaction_tile.dart';

/// Balance card for the rider wallet (Phase H Step 1:
/// "Balance Screen: Show Available, Pending, Held").
class BalanceCard extends StatelessWidget {
  final LedgerBalance balance;

  const BalanceCard({super.key, required this.balance});

  @override
  Widget build(BuildContext context) {
    return Card(
      elevation: 2,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(16)),
      child: Container(
        padding: const EdgeInsets.all(20),
        decoration: BoxDecoration(
          borderRadius: BorderRadius.circular(16),
          gradient: const LinearGradient(
            colors: [AppColors.primaryDark, AppColors.primary],
            begin: Alignment.topLeft,
            end: Alignment.bottomRight,
          ),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              mainAxisAlignment: MainAxisAlignment.spaceBetween,
              children: [
                const Text(
                  'Available Balance',
                  style: TextStyle(color: Colors.white70, fontSize: 14),
                ),
                if (balance.isLocked)
                  _StatusChip(
                    label: balance.status == 'negative_lock'
                        ? 'Account locked — repay to continue'
                        : 'Account frozen',
                    color: AppColors.errorLight,
                  ),
              ],
            ),
            const SizedBox(height: 8),
            Text(
              formatEtb(balance.available),
              style: const TextStyle(
                color: Colors.white,
                fontSize: 34,
                fontWeight: FontWeight.bold,
              ),
            ),
            const SizedBox(height: 20),
            Row(
              children: [
                Expanded(
                  child: _BalanceStat(
                    label: 'Pending',
                    value: formatEtb(balance.pending),
                    tooltip:
                        'Funds from top-ups awaiting provider confirmation. '
                        'Failed captures are clawed back automatically.',
                  ),
                ),
                Expanded(
                  child: _BalanceStat(
                    label: 'Held',
                    value: formatEtb(balance.held),
                    tooltip:
                        'Amounts inside the 72-hour ride escrow safety window.',
                  ),
                ),
                Expanded(
                  child: _BalanceStat(
                    label: 'Withdrawable',
                    value: formatEtb(balance.withdrawable),
                    tooltip: 'Funds eligible for payout right now.',
                  ),
                ),
              ],
            ),
            const SizedBox(height: 16),
            // Audit anchor: hash-chain head of this user's ledger history.
            Row(
              mainAxisAlignment: MainAxisAlignment.spaceBetween,
              children: [
                Text(
                  'Ledger head: ${shortHash(balance.chainHeadHash)}',
                  style: const TextStyle(
                      color: Colors.white38, fontSize: 11),
                ),
                Text(
                  'v${balance.version}',
                  style: const TextStyle(
                      color: Colors.white38, fontSize: 11),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

class _BalanceStat extends StatelessWidget {
  final String label;
  final String value;
  final String tooltip;

  const _BalanceStat({
    required this.label,
    required this.value,
    required this.tooltip,
  });

  @override
  Widget build(BuildContext context) {
    return Tooltip(
      message: tooltip,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(label,
              style: const TextStyle(color: Colors.white70, fontSize: 12)),
          const SizedBox(height: 4),
          Text(
            value,
            style: const TextStyle(
              color: Colors.white,
              fontSize: 16,
              fontWeight: FontWeight.w600,
            ),
          ),
        ],
      ),
    );
  }
}

class _StatusChip extends StatelessWidget {
  final String label;
  final Color color;

  const _StatusChip({required this.label, required this.color});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
      decoration: BoxDecoration(
        color: color.withOpacity(0.2),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(Icons.lock_outline, size: 14, color: color),
          const SizedBox(width: 4),
          Text(label,
              style: TextStyle(color: color, fontSize: 11)),
        ],
      ),
    );
  }
}
