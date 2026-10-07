part of 'wallet_bloc.dart';

// ============================================================================
// EVENTS
// ============================================================================

abstract class WalletEvent extends Equatable {
  const WalletEvent();

  @override
  List<Object?> get props => [];
}

/// Load balance + first page of transactions (wallet screen entry).
class LoadWallet extends WalletEvent {
  const LoadWallet();
}

/// Re-fetch only the cached balance (pull-to-refresh / post-payment).
class RefreshBalance extends WalletEvent {
  const RefreshBalance();
}

/// Append the next page of transaction history.
class LoadMoreTransactions extends WalletEvent {
  const LoadMoreTransactions();
}

/// Start a fiat on-ramp over Telebirr / Chapa / M-Pesa Ethiopia.
class StartTopup extends WalletEvent {
  final LedgerMoney amount;
  final PaymentProvider provider;
  final Map<String, dynamic>? metadata;

  const StartTopup({
    required this.amount,
    required this.provider,
    this.metadata,
  });

  @override
  List<Object?> get props => [amount.cents, provider, metadata];
}

/// File a dispute against a completed ride (Phase F: within 72h window).
class FileRideDispute extends WalletEvent {
  final String rideId;
  final String reasonCode;
  final String description;
  final List<String> evidenceUrls;

  const FileRideDispute({
    required this.rideId,
    required this.reasonCode,
    required this.description,
    this.evidenceUrls = const [],
  });

  @override
  List<Object?> get props => [rideId, reasonCode, description, evidenceUrls];
}
