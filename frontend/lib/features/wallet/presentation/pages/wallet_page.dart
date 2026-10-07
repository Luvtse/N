import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';

import '../../../../core/theme/app_colors.dart';
import '../../domain/entities/ledger_models.dart';
import '../bloc/wallet_bloc.dart';
import '../widgets/balance_card.dart';
import '../widgets/topup_sheet.dart';
import '../widgets/transaction_tile.dart';
import '../widgets/dispute_form_sheet.dart';

/// Rider Wallet screen (Phase H Step 1): balance breakdown, top-up entry
/// point and hash-linked transaction history with infinite scroll.
class WalletPage extends StatelessWidget {
  /// Optional ride id — when provided (e.g. pushed from ride history), a
  /// "File a dispute" shortcut is shown after load.
  final String? disputeRideId;

  const WalletPage({super.key, this.disputeRideId});

  @override
  Widget build(BuildContext context) {
    return BlocConsumer<WalletBloc, WalletState>(
      // When pushed here from ride history with a disputeRideId, open the
      // dispute form automatically once the wallet has loaded.
      listenWhen: (previous, current) =>
          previous is! WalletLoaded && current is WalletLoaded,
      listener: (context, state) {
        final rideId = disputeRideId;
        if (rideId != null && state is WalletLoaded) {
          WidgetsBinding.instance.addPostFrameCallback((_) {
            if (context.mounted) _onDisputePressed(context, rideId);
          });
        }
      },
      buildWhen: (previous, current) => true,
      builder: (context, state) => _buildScaffold(context, state),
    );
  }

  Widget _buildScaffold(BuildContext context, WalletState state) {
    return BlocListener<WalletBloc, WalletState>(
      listener: (context, state) {
        if (state is TopupSubmitted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(
              content: Text(
                'Top-up of ${state.result.amount} ETB accepted via '
                '${state.result.provider}. Confirmation pending.',
              ),
              backgroundColor: AppColors.success,
            ),
          );
        } else if (state is TopupFailure) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(
              content: Text(
                'Top-up failed${state.code != null ? ' (${state.code})' : ''}: '
                '${state.message}',
              ),
              backgroundColor: AppColors.error,
            ),
          );
        } else if (state is DisputeFiled) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(
              content: Text(state.autoResolved
                  ? 'Dispute resolved automatically — refund issued.'
                  : 'Dispute filed. Our team will review it shortly.'),
              backgroundColor: AppColors.primary,
            ),
          );
        } else if (state is DisputeFailure) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(
              content: Text('Could not file dispute: ${state.message}'),
              backgroundColor: AppColors.error,
            ),
          );
        }
      },
      child: Scaffold(
        backgroundColor: AppColors.background,
        appBar: AppBar(
          title: const Text('Wallet'),
          backgroundColor: Colors.white,
          elevation: 0,
          actions: [
            IconButton(
              icon: const Icon(Icons.account_balance_wallet_outlined),
              tooltip: 'Add Money',
              onPressed: () => _onTopUpPressed(context),
            ),
          ],
        ),
        floatingActionButton: FloatingActionButton.extended(
          heroTag: 'wallet-topup-fab',
          backgroundColor: AppColors.primary,
          foregroundColor: Colors.white,
          onPressed: () => _onTopUpPressed(context),
          icon: const Icon(Icons.add),
          label: const Text('Add Money'),
        ),
        body: BlocBuilder<WalletBloc, WalletState>(
          builder: (context, state) {
            if (state is WalletLoading || state is WalletInitial) {
              return const Center(child: CircularProgressIndicator());
            }

            if (state is WalletError) {
              return _ErrorBody(message: state.message, code: state.code);
            }

            // While a top-up/dispute sub-flow is running, keep showing the
            // last loaded wallet underneath.
            final loaded = state is WalletLoaded
                ? state
                : _lastKnownLoaded(context);
            if (loaded == null) {
              return _ErrorBody(message: 'Wallet unavailable', code: null);
            }

            return RefreshIndicator(
              onRefresh: () async =>
                  context.read<WalletBloc>().add(const RefreshBalance()),
              child: CustomScrollView(
                slivers: [
                  SliverPadding(
                    padding: const EdgeInsets.all(16),
                    sliver: SliverToBoxAdapter(
                      child: BalanceCard(balance: loaded.balance),
                    ),
                  ),
                  // Dispute shortcut when opened from ride history.
                  if (disputeRideId != null)
                    SliverPadding(
                      padding: const EdgeInsets.symmetric(horizontal: 16),
                      sliver: SliverToBoxAdapter(
                        child: OutlinedButton.icon(
                          style: OutlinedButton.styleFrom(
                            foregroundColor: AppColors.error,
                            side: const BorderSide(color: AppColors.error),
                            minimumSize: const Size.fromHeight(44),
                          ),
                          icon: const Icon(Icons.gavel),
                          label: const Text('File a dispute for this ride'),
                          onPressed: () => _onDisputePressed(
                              context, disputeRideId!),
                        ),
                      ),
                    ),
                  SliverToBoxAdapter(
                    child: Padding(
                      padding: const EdgeInsets.fromLTRB(16, 8, 16, 8),
                      child: Row(
                        mainAxisAlignment: MainAxisAlignment.spaceBetween,
                        children: [
                          const Text(
                            'Transactions',
                            style: TextStyle(
                                fontSize: 16, fontWeight: FontWeight.bold),
                          ),
                          Text(
                            '${loaded.transactions.items.length} / '
                            '${loaded.transactions.total}',
                            style: TextStyle(
                                fontSize: 12, color: Colors.grey[600]),
                          ),
                        ],
                      ),
                    ),
                  ),
                  SliverList(
                    delegate: SliverChildBuilderDelegate(
                      (context, index) {
                        if (index >= loaded.transactions.items.length) {
                          return loaded.isLoadingMore
                              ? const Padding(
                                  padding: EdgeInsets.all(16),
                                  child: Center(
                                      child:
                                          CircularProgressIndicator()),
                                )
                              : const SizedBox(height: 80); // FAB space
                        }
                        final tx = loaded.transactions.items[index];
                        return _TransactionRow(tx: tx);
                      },
                      childCount: loaded.transactions.items.length + 1,
                    ),
                  ),
                ],
              ),
            );
          },
        ),
      ),
    );
  }

  /// Fetch the most recent WalletLoaded even while transient states are
  /// active (BlocBuilder would otherwise hide the list during top-ups).
  WalletLoaded? _lastKnownLoaded(BuildContext context) {
    final bloc = context.read<WalletBloc>();
    // Walk emitted states via the bloc's latest value fallback: try `state`.
    final current = bloc.state;
    if (current is WalletLoaded) return current;
    return null;
  }

  Future<void> _onTopUpPressed(BuildContext context) async {
    final request = await TopupSheet.show(context);
    if (request == null || !context.mounted) return;
    context.read<WalletBloc>().add(
          StartTopup(amount: request.amount, provider: request.provider),
        );
  }

  /// Open the dispute form sheet and dispatch [FileRideDispute] on submit.
  Future<void> _onDisputePressed(BuildContext context, String rideId) async {
    final submission = await DisputeFormSheet.show(context, rideId: rideId);
    if (submission == null || !context.mounted) return;
    context.read<WalletBloc>().add(FileRideDispute(
          rideId: rideId,
          reasonCode: submission.reasonCode,
          description: submission.description,
          evidenceUrls: submission.evidenceUrls,
        ));
  }
}

// ============================================================================
// TRANSACTION ROW
// ============================================================================

class _TransactionRow extends StatelessWidget {
  final LedgerTransaction tx;

  const _TransactionRow({required this.tx});

  @override
  Widget build(BuildContext context) {
    final visual = TransactionTypeVisual.of(tx.type);
    final amount = tx.amount;
    return Card(
      margin: const EdgeInsets.symmetric(horizontal: 16, vertical: 4),
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
      child: ListTile(
        leading: CircleAvatar(
          backgroundColor: visual.color.withOpacity(0.12),
          child: Icon(visual.icon, color: visual.color, size: 20),
        ),
        title: Text(visual.label,
            style: const TextStyle(fontWeight: FontWeight.w600)),
        subtitle: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              '${_formatDate(tx.createdAt)}'
              '${tx.description != null ? ' · ${tx.description}' : ''}',
              style: const TextStyle(fontSize: 12),
            ),
            // Hash link so users can see chain continuity (audit UX).
            Text(
              '#${shortHash(tx.txHash)} ← ${shortHash(tx.prevHash)}',
              style: const TextStyle(fontSize: 10, color: Colors.black38),
            ),
          ],
        ),
        trailing: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          crossAxisAlignment: CrossAxisAlignment.end,
          children: [
            Text(
              formatEtb(amount, signed: true),
              style: TextStyle(
                fontWeight: FontWeight.bold,
                color: amount.cents >= 0
                    ? AppColors.success
                    : AppColors.error,
              ),
            ),
            const SizedBox(height: 2),
            Text(
              'bal ${formatEtb(tx.balanceAfter)}',
              style: const TextStyle(fontSize: 10, color: Colors.black45),
            ),
          ],
        ),
      ),
    );
  }

  String _formatDate(DateTime d) {
    final local = d.toLocal();
    String two(int n) => n.toString().padLeft(2, '0');
    return '${local.year}-${two(local.month)}-${two(local.day)} '
        '${two(local.hour)}:${two(local.minute)}';
  }
}

// ============================================================================
// ERROR BODY
// ============================================================================

class _ErrorBody extends StatelessWidget {
  final String message;
  final String? code;

  const _ErrorBody({required this.message, required this.code});

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(32),
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            const Icon(Icons.error_outline,
                size: 56, color: AppColors.error),
            const SizedBox(height: 16),
            Text(
              code == null ? message : '[$code] $message',
              textAlign: TextAlign.center,
            ),
            const SizedBox(height: 24),
            ElevatedButton.icon(
              onPressed: () =>
                  context.read<WalletBloc>().add(const LoadWallet()),
              icon: const Icon(Icons.refresh),
              label: const Text('Retry'),
            ),
          ],
        ),
      ),
    );
  }
}
