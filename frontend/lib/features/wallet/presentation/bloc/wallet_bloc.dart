import 'package:equatable/equatable.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:uuid/uuid.dart';

import '../../data/repositories/wallet_repository.dart';
import '../../domain/entities/ledger_models.dart';

part 'wallet_event.dart';
part 'wallet_state.dart';

/// Drives the rider Wallet slice (Phase H Step 1): balance screen, top-up
/// flow over the Ethiopian rails, paginated transaction history and disputes.
class WalletBloc extends Bloc<WalletEvent, WalletState> {
  final WalletRepository _repository;
  final Uuid _uuid;

  /// Last loaded page size — kept so LoadMoreTransactions stays consistent.
  static const int pageSize = 50;

  WalletBloc({
    required WalletRepository repository,
    Uuid? uuid,
  })  : _repository = repository,
        _uuid = uuid ?? const Uuid(),
        super(const WalletInitial()) {
    on<LoadWallet>(_onLoadWallet);
    on<RefreshBalance>(_onRefreshBalance);
    on<LoadMoreTransactions>(_onLoadMoreTransactions);
    on<StartTopup>(_onStartTopup);
    on<FileRideDispute>(_onFileDispute);
  }

  // ------------------------------------------------------------------
  // Full load: balance + first transactions page, in parallel.
  // ------------------------------------------------------------------
  Future<void> _onLoadWallet(
    LoadWallet event,
    Emitter<WalletState> emit,
  ) async {
    emit(WalletLoading());
    try {
      final results = await Future.wait([
        _repository.getBalance(),
        _repository.listTransactions(limit: pageSize, offset: 0),
      ]);
      emit(WalletLoaded(
        balance: results[0] as LedgerBalance,
        transactions: results[1] as TransactionPage,
      ));
    } on ApiException catch (e) {
      emit(WalletError(message: e.message, code: e.errorCode));
    } catch (e) {
      emit(WalletError(message: 'Failed to load wallet: $e'));
    }
  }

  Future<void> _onRefreshBalance(
    RefreshBalance event,
    Emitter<WalletState> emit,
  ) async {
    final current = state;
    if (current is! WalletLoaded) return;
    emit(current.copyWith(isRefreshing: true));
    try {
      final balance = await _repository.getBalance();
      emit(current.copyWith(balance: balance, isRefreshing: false));
    } on ApiException catch (e) {
      emit(current.copyWith(isRefreshing: false));
      emit(WalletError(message: e.message, code: e.errorCode));
    } catch (e) {
      emit(current.copyWith(isRefreshing: false));
      emit(WalletError(message: 'Failed to refresh balance: $e'));
    }
  }

  Future<void> _onLoadMoreTransactions(
    LoadMoreTransactions event,
    Emitter<WalletState> emit,
  ) async {
    final current = state;
    if (current is! WalletLoaded || !current.transactions.hasMore) return;
    emit(current.copyWith(isLoadingMore: true));
    try {
      final next = await _repository.listTransactions(
        limit: pageSize,
        offset: current.transactions.offset + current.transactions.items.length,
      );
      final merged = TransactionPage(
        items: [...current.transactions.items, ...next.items],
        total: next.total,
        limit: next.limit,
        offset: current.transactions.offset,
      );
      emit(current.copyWith(transactions: merged, isLoadingMore: false));
    } on ApiException catch (e) {
      emit(current.copyWith(isLoadingMore: false));
      emit(WalletError(message: e.message, code: e.errorCode));
    } catch (e) {
      emit(current.copyWith(isLoadingMore: false));
      emit(WalletError(message: 'Failed to load more transactions: $e'));
    }
  }

  // ------------------------------------------------------------------
  // Top-up (Phase E Step 3 optimistic flow). The backend credits the
  // balance immediately and finalises asynchronously when the provider
  // webhook/poll confirms; on failure it posts a negative adjustment and
  // may set status=negative_lock — surfaced here via the balance refresh.
  // ------------------------------------------------------------------
  Future<void> _onStartTopup(
    StartTopup event,
    Emitter<WalletState> emit,
  ) async {
    final current = state as WalletLoaded?;
    emit(TopupSubmitting(amount: event.amount, provider: event.provider));
    try {
      final result = await _repository.requestTopup(
        amount: event.amount,
        provider: event.provider,
        idempotencyKey: _uuid.v4(),
        metadata: event.metadata,
      );
      // Re-fetch balance so the optimistic credit is reflected instantly.
      LedgerBalance? refreshed;
      try {
        refreshed = await _repository.getBalance();
      } catch (_) {
        refreshed = null; // non-fatal: history screen refreshes later
      }
      emit(TopupSubmitted(result: result, refreshedBalance: refreshed));
    } on ApiException catch (e) {
      emit(TopupFailure(
        message: e.message,
        code: e.errorCode,
        retryable: e is ServerException || e is NetworkException ||
            e.errorCode == 'TRY_AGAIN',
      ));
    } catch (e) {
      emit(TopupFailure(message: 'Top-up failed: $e', retryable: true));
    }
    // Keep a loaded wallet around for the screens behind the sheet.
    if (state is! TopupSubmitted && current != null) {
      add(const RefreshBalance());
    }
  }

  // ------------------------------------------------------------------
  // Dispute filing (Phase F workflow entry point from Phase H UI).
  // ------------------------------------------------------------------
  Future<void> _onFileDispute(
    FileRideDispute event,
    Emitter<WalletState> emit,
  ) async {
    emit(const DisputeSubmitting());
    try {
      final result = await _repository.fileDispute(
        rideId: event.rideId,
        reasonCode: event.reasonCode,
        description: event.description,
        evidenceUrls: event.evidenceUrls,
      );
      emit(DisputeFiled(result));
    } on ApiException catch (e) {
      emit(DisputeFailure(
        message: e.message,
        code: e.errorCode,
        // 409/410 mean "already disputed" / "window closed" — not retryable.
        retryable: e.statusCode != 409 && e.statusCode != 410,
      ));
    } catch (e) {
      emit(DisputeFailure(message: 'Could not file dispute: $e'));
    }
  }
}
