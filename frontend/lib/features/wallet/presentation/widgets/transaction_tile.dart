import 'package:flutter/material.dart';

import '../../../../core/theme/app_colors.dart';
import '../../domain/entities/ledger_models.dart';

/// Visual mapping for ledger transaction types (Phase H Step 1:
/// "History: List transactions with icons for Type"). Keep in sync with the
/// `type` CHECK constraint in database/migrations/postgres/004_ledger_schema.sql.
class TransactionTypeVisual {
  final IconData icon;
  final Color color;
  final String label;

  const TransactionTypeVisual({
    required this.icon,
    required this.color,
    required this.label,
  });

  static const Map<String, TransactionTypeVisual> _byType = {
    'topup': TransactionTypeVisual(
      icon: Icons.add_circle_outline,
      color: AppColors.success,
      label: 'Top-up',
    ),
    'ride_debit': TransactionTypeVisual(
      icon: Icons.directions_car,
      color: AppColors.primary,
      label: 'Ride payment',
    ),
    'ride_credit_held': TransactionTypeVisual(
      icon: Icons.lock_clock,
      color: AppColors.warning,
      label: 'Ride earning (on hold)',
    ),
    'escrow_release': TransactionTypeVisual(
      icon: Icons.unlock,
      color: AppColors.success,
      label: 'Escrow released',
    ),
    'refund': TransactionTypeVisual(
      icon: Icons.replay_circle_filled,
      color: AppColors.secondary,
      label: 'Refund',
    ),
    'withdrawal_debit': TransactionTypeVisual(
      icon: Icons.money_off_outlined,
      color: AppColors.error,
      label: 'Withdrawal',
    ),
    'withdrawal_reversal': TransactionTypeVisual(
      icon: Icons.undo,
      color: AppColors.warning,
      label: 'Withdrawal reversed',
    ),
    'adjustment_credit': TransactionTypeVisual(
      icon: Icons.tune,
      color: AppColors.success,
      label: 'Adjustment (+)',
    ),
    'adjustment_debit': TransactionTypeVisual(
      icon: Icons.tune,
      color: AppColors.error,
      label: 'Adjustment (-)',
    ),
    'chargeback': TransactionTypeVisual(
      icon: Icons.report_gmailerrorred,
      color: AppColors.error,
      label: 'Chargeback',
    ),
  };

  static const TransactionTypeVisual _fallback = TransactionTypeVisual(
    icon: Icons.swap_horiz,
    color: Colors.grey,
    label: 'Transaction',
  );

  static TransactionTypeVisual of(String type) =>
      _byType[type.toLowerCase()] ?? _fallback;
}

/// Formats ETB money exactly like the backend renders it (2 decimals).
String formatEtb(LedgerMoney money, {bool signed = false}) {
  final value = money.etb.abs();
  final text = value.toStringAsFixed(2);
  if (!signed) return 'ETB $text';
  final sign = money.isNegative ? '-' : '+';
  return '$sign ETB $text';
}

/// Shorten a SHA-256 hex hash for display in the history list.
String shortHash(String hash) {
  if (hash.length <= 12) return hash;
  return '${hash.substring(0, 6)}…${hash.substring(hash.length - 4)}';
}
