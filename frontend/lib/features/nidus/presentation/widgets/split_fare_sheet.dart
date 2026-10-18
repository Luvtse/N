import 'package:flutter/material.dart';

import '../../../../core/di/injection.dart';
import '../../data/repositories/split_fare_repository.dart';

/// §3.3 Split Fare sheet, opened from the receipt page.
///
/// Behavior contract (mirrors the backend split service):
///  * Creating invites does NOT change the inviter's charge — the server
///    reduces the fare only by ACCEPTED shares. The sheet therefore shows
///    "waiting for X to accept" states sourced from GET /rides/{id}/split,
///    never a client-side guess of who has paid.
///  * Every number on screen comes from the server DTO (fare, inviter_share,
///    total_split, remaining_to_pay). If the fetch fails we show an honest
///    "unavailable — retry" state; we never invent amounts (Global Rule).
class SplitFareSheet extends StatefulWidget {
  final String rideId;
  final double fare;
  final String currency;

  const SplitFareSheet({
    super.key,
    required this.rideId,
    required this.fare,
    required this.currency,
  });

  static Future<void> open(
    BuildContext context, {
    required String rideId,
    required double fare,
    required String currency,
  }) {
    return showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      builder: (_) => FractionallySizedBox(
        heightFactor: 0.85,
        child: SplitFareSheet(
          rideId: rideId,
          fare: fare,
          currency: currency,
        ),
      ),
    );
  }

  @override
  State<SplitFareSheet> createState() => _SplitFareSheetState();
}

class _SplitFareSheetState extends State<SplitFareSheet> {
  final _repo = getIt<SplitFareRepository>();
  final _formKey = GlobalKey<FormState>();

  final List<_ShareDraft> _drafts = [];
  SplitStatus? _status;
  bool _loading = true;
  bool _submitting = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _loadStatus();
  }

  Future<void> _loadStatus() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    final result = await _repo.getSplit(widget.rideId);
    if (!mounted) return;
    setState(() {
      _loading = false;
      if (result.isSuccess) {
        _status = result.status;
      } else {
        // A 404 / no-split-yet is not an error surface: exists=false simply
        // means nobody has been invited yet. Anything else shows retry.
        _status = null;
        _error = result.failureCode == 'SPLIT_NOT_FOUND' ||
                result.failureCode == 'NOT_FOUND'
            ? null
            : result.failureMessage;
      }
    });
  }

  Future<void> _submit() async {
    // Server-side invariant: sum(shares) must be < fare. We pre-check only to
    // give instant feedback; the backend remains authoritative and its error
    // is surfaced verbatim on rejection.
    final total = _drafts.fold<double>(0, (s, d) => s + d.amount);
    if (_drafts.isEmpty || total <= 0) {
      setState(() => _error = 'Add at least one share before sending.');
      return;
    }
    if (!_formKey.currentState!.validate()) return;
    if (total >= widget.fare) {
      setState(() => _error =
          'Shares must add up to less than the fare (${_fmt(widget.fare)}).');
      return;
    }

    setState(() {
      _submitting = true;
      _error = null;
    });
    final result = await _repo.createSplit(
      widget.rideId,
      _drafts
          .map((d) => SplitShareRequest(contact: d.contact, shareAmount: d.amount))
          .toList(),
    );
    if (!mounted) return;
    setState(() {
      _submitting = false;
      if (result.isSuccess) {
        _status = result.status;
        _drafts.clear();
      } else {
        _error = result.failureMessage ?? 'Could not send invites — try again.';
      }
    });
  }

  String _fmt(double v) => '${widget.currency} ${v.toStringAsFixed(2)}';

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: EdgeInsets.only(
        left: 16,
        right: 16,
        top: 16,
        bottom: MediaQuery.of(context).viewInsets.bottom + 16,
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              Text('Split fare', style: theme.textTheme.titleLarge),
              const Spacer(),
              IconButton(
                icon: const Icon(Icons.close),
                onPressed: () => Navigator.of(context).pop(),
              ),
            ],
          ),
          Text(
            'Invites are sent by email/SMS. Your charge drops only as people '
            'accept — the totals below come straight from the server.',
            style: theme.textTheme.bodySmall,
          ),
          const SizedBox(height: 12),
          Expanded(
            child: RefreshIndicator(
              onRefresh: _loadStatus,
              child: ListView(
                children: [
                  _serverSummary(theme),
                  const SizedBox(height: 16),
                  Form(
                    key: _formKey,
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: [
                        for (var i = 0; i < _drafts.length; i++) ...[
                          _draftRow(i),
                          const SizedBox(height: 8),
                        ],
                        OutlinedButton.icon(
                          onPressed: _submitting
                              ? null
                              : () => setState(() => _drafts.add(
                                    _ShareDraft(contact: '', amount: 0),
                                  )),
                          icon: const Icon(Icons.person_add_alt),
                          label: const Text('Add person'),
                        ),
                        const SizedBox(height: 16),
                        if (_drafts.isNotEmpty)
                          FilledButton(
                            onPressed: _submitting ? null : _submit,
                            child: _submitting
                                ? const SizedBox(
                                    height: 18,
                                    width: 18,
                                    child: CircularProgressIndicator(
                                        strokeWidth: 2),
                                  )
                                : Text('Send invites · ${_fmt(_drafts.fold<double>(
                                    0, (s, d) => s + d.amount))}'),
                          ),
                      ],
                    ),
                  ),
                  if (_error != null) ...[
                    const SizedBox(height: 12),
                    _ErrorBanner(message: _error!, onRetry: _loadStatus),
                  ],
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _serverSummary(ThemeData theme) {
    if (_loading) {
      return const Padding(
        padding: EdgeInsets.symmetric(vertical: 24),
        child: Center(child: CircularProgressIndicator()),
      );
    }
    final status = _status;
    if (status == null || !status.exists) {
      return Card(
        child: ListTile(
          leading: const Icon(Icons.group_add_outlined),
          title: const Text('Not split yet'),
          subtitle: Text('Fare ${_fmt(widget.fare)} — you are paying the full '
              'amount until someone accepts an invite.'),
        ),
      );
    }
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('Split status', style: theme.textTheme.titleMedium),
            const SizedBox(height: 8),
            _kv('Fare', _fmt(status.fare)),
            _kv('Your share after accepted invites', _fmt(status.inviterShare)),
            _kv('Total invited', _fmt(status.totalSplit)),
            _kv('Still waiting', _fmt(status.remainingToPay)),
            const Divider(height: 24),
            for (final invitee in status.invitees)
              ListTile(
                dense: true,
                contentPadding: EdgeInsets.zero,
                leading: Icon(_statusIcon(invitee.status)),
                title: Text(invitee.inviteeContact),
                subtitle: Text('${_fmt(invitee.shareAmount)} · '
                    '${invitee.status.replaceAll('_', ' ')}'),
              ),
          ],
        ),
      ),
    );
  }

  IconData _statusIcon(String status) => switch (status) {
        'paid' => Icons.check_circle,
        'accepted' => Icons.how_to_reg,
        'declined' => Icons.do_not_disturb_on,
        'expired' => Icons.timer_off,
        _ => Icons.hourglass_empty,
      };

  Widget _kv(String k, String v) => Padding(
        padding: const EdgeInsets.symmetric(vertical: 2),
        child: Row(
          mainAxisAlignment: MainAxisAlignment.spaceBetween,
          children: [
            Text(k, style: const TextStyle(color: Colors.grey)),
            Text(v, style: const TextStyle(fontWeight: FontWeight.w600)),
          ],
        ),
      );

  Widget _draftRow(int index) {
    final draft = _drafts[index];
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Expanded(
          flex: 3,
          child: TextFormField(
            initialValue: draft.contact,
            decoration: const InputDecoration(
              labelText: 'Email or phone',
              border: OutlineInputBorder(),
              isDense: true,
            ),
            validator: (v) => (v == null || v.trim().isEmpty)
                ? 'Required'
                : (v.contains('@') ||
                        RegExp(r'^\+?[0-9 \-()]{7,18}$').hasMatch(v.trim()))
                    ? null
                    : 'Enter a valid email or phone number',
            onChanged: (v) => setState(() => draft.contact = v.trim()),
          ),
        ),
        const SizedBox(width: 8),
        Expanded(
          flex: 2,
          child: TextFormField(
            initialValue: draft.amount > 0 ? draft.amount.toString() : '',
            keyboardType: TextInputType.numberWithOptions(decimal: true),
            decoration: InputDecoration(
              labelText: 'Share (${widget.currency})',
              border: const OutlineInputBorder(),
              isDense: true,
            ),
            validator: (v) {
              final parsed = double.tryParse(v ?? '');
              if (parsed == null || parsed <= 0) return 'Invalid';
              if (parsed >= widget.fare) return '≥ fare';
              return null;
            },
            onChanged: (v) =>
                setState(() => draft.amount = double.tryParse(v) ?? 0),
          ),
        ),
        IconButton(
          icon: const Icon(Icons.remove_circle_outline),
          onPressed: _submitting
              ? null
              : () => setState(() => _drafts.removeAt(index)),
        ),
      ],
    );
  }
}

class _ShareDraft {
  String contact;
  double amount;
  _ShareDraft({required this.contact, required this.amount});
}

class _ErrorBanner extends StatelessWidget {
  final String message;
  final VoidCallback onRetry;

  const _ErrorBanner({required this.message, required this.onRetry});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: Theme.of(context).colorScheme.errorContainer,
        borderRadius: BorderRadius.circular(8),
      ),
      child: Row(
        children: [
          const Icon(Icons.error_outline),
          const SizedBox(width: 8),
          Expanded(child: Text(message)),
          TextButton(onPressed: onRetry, child: const Text('Retry')),
        ],
      ),
    );
  }
}
