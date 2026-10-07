part of 'wallet_bloc.dart';

// ============================================================================
// STATES
// ============================================================================

abstract class WalletState extends Equatable {
  const WalletState();

  @override
  List<Object?> get props => [];
}

class WalletInitial extends WalletState {
  const WalletInitial();
}

class WalletLoading extends WalletState {
  const WalletLoading();
}

/// Balance + transaction history ready.
class WalletLoaded extends WalletState {
  final LedgerBalance balance;
  final TransactionPage transactions;
  final bool isRefreshing;
  final bool isLoadingMore;

  const WalletLoaded({
    required this.balance,
    required this.transactions,
    this.isRefreshing = false,
    this.isLoadingMore = false,
  });

  WalletLoaded copyWith({
    LedgerBalance? balance,
    TransactionPage? transactions,
    bool? isRefreshing,
    bool? isLoadingMore,
  }) {
    return WalletLoaded(
      balance: balance ?? this.balance,
      transactions: transactions ?? this.transactions,
      isRefreshing: isRefreshing ?? this.isRefreshing,
      isLoadingMore: isLoadingMore ?? this.isLoadingMore,
    );
  }

  @override
  List<Object?> get props =>
      [balance, transactions, isRefreshing, isLoadingMore];
}

/// Top-up in flight (optimistic credit happens server-side).
class TopupSubmitting extends WalletState {
  final LedgerMoney amount;
  final PaymentProvider provider;

  const TopupSubmitting({required this.amount, required this.provider});

  @override
  List<Object?> get props => [amount, provider];
}

/// Top-up accepted by the ledger; awaiting async provider confirmation.
class TopupSubmitted extends WalletState {
  final TopupResult result;

  /// Fresh balance after the optimistic credit (null if re-fetch failed).
  final LedgerBalance? refreshedBalance;

  const TopupSubmitted({required this.result, this.refreshedBalance});

  @override
  List<Object?> get props => [result, refreshedBalance];
}

class TopupFailure extends WalletState {
  final String message;
  final String? code;
  final bool retryable;

  const TopupFailure({
    required this.message,
    this.code,
    this.retryable = false,
  });

  @override
  List<Object?> get props => [message, code, retryable];
}

class DisputeSubmitting extends WalletState {
  const DisputeSubmitting();
}

class DisputeFiled extends WalletState {
  final DisputeResult dispute;

  const DisputeFiled(this.dispute);

  /// Simple cases (e.g. no-show) auto-resolve with an immediate refund.
  bool get autoResolved =>
      dispute.status == 'resolved_rider' ||
      dispute.status == 'auto_resolved';

  @override
  List<Object?> get props => [dispute];
}

class DisputeFailure extends WalletState {
  final String message;
  final String? code;
  final bool retryable;

  const DisputeFailure({
    required this.message,
    this.code,
    this.retryable = false,
  });

  @override
  List<Object?> get props => [message, code, retryable];
}

/// Terminal error state for the whole wallet screen.
class WalletError extends WalletState {
  final String message;
  final String? code;

  const WalletError({required this.message, this.code});

  @override
  List<Object?> get props => [message, code];
}
