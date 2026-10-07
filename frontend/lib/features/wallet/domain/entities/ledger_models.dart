/// Domain entities for the NIDAW private ledger (Phase D backend contract).
///
/// Mirrors the DTOs exposed by
/// `backend/internal/modules/ledger/interfaces/http/handler.go`. All money
/// values arrive as ETB decimal *strings* from the API (never floats) to keep
/// cent-exact arithmetic; [LedgerMoney] parses them into cents (int) locally.
class LedgerMoney {
  /// Amount in whole ETB cents (1 ETB = 100 cents). Signed.
  final int cents;

  const LedgerMoney(this.cents);

  /// Parse an API decimal string such as "12.34" or "-0.50" into cents.
  /// Returns [LedgerMoney.zero] for null/garbage input rather than throwing,
  /// so a malformed server field degrades to 0 instead of crashing the UI.
  factory LedgerMoney.parse(String? raw) {
    if (raw == null || raw.trim().isEmpty) return LedgerMoney.zero;
    final value = double.tryParse(raw.trim());
    if (value == null) return LedgerMoney.zero;
    // Round at the cent level to avoid float drift on two-decimal inputs.
    return LedgerMoney((value * 100).round());
  }

  static const zero = LedgerMoney(0);

  bool get isPositive => cents > 0;
  bool get isNegative => cents < 0;

  double get etb => cents / 100.0;

  /// Render like the backend does: plain decimal with exactly 2 fraction digits.
  @override
  String toString() => etb.toStringAsFixed(2);
}

/// GET /api/v1/ledger/balance — cached user_balances row.
class LedgerBalance {
  final String userId;
  final LedgerMoney available;
  final LedgerMoney pending;
  final LedgerMoney held;
  final LedgerMoney withdrawable;
  final LedgerMoney total;
  final String currency;

  /// 'active' | 'negative_lock' | 'frozen' (see entities.UserBalance.Status).
  final String status;

  /// Head hash of this user's transaction hash-chain (audit anchor).
  final String chainHeadHash;
  final int version;
  final DateTime updatedAt;

  const LedgerBalance({
    required this.userId,
    required this.available,
    required this.pending,
    required this.held,
    required this.withdrawable,
    required this.total,
    required this.currency,
    required this.status,
    required this.chainHeadHash,
    required this.version,
    required this.updatedAt,
  });

  bool get isLocked => status == 'negative_lock' || status == 'frozen';

  factory LedgerBalance.fromJson(Map<String, dynamic> json) {
    return LedgerBalance(
      userId: json['user_id'] as String? ?? '',
      available: LedgerMoney.parse(json['available_etb'] as String?),
      pending: LedgerMoney.parse(json['pending_etb'] as String?),
      held: LedgerMoney.parse(json['held_etb'] as String?),
      withdrawable: LedgerMoney.parse(json['withdrawable_etb'] as String?),
      total: LedgerMoney.parse(json['total_etb'] as String?),
      currency: json['currency'] as String? ?? 'ETB',
      status: json['status'] as String? ?? 'active',
      chainHeadHash: json['chain_head_hash'] as String? ?? '',
      version: (json['version'] as num?)?.toInt() ?? 0,
      updatedAt: DateTime.tryParse(json['updated_at'] as String? ?? '') ??
          DateTime.fromMillisecondsSinceEpoch(0),
    );
  }

  Map<String, dynamic> toJson() => {
        'user_id': userId,
        'available_etb': available.toString(),
        'pending_etb': pending.toString(),
        'held_etb': held.toString(),
        'withdrawable_etb': withdrawable.toString(),
        'total_etb': total.toString(),
        'currency': currency,
        'status': status,
        'chain_head_hash': chainHeadHash,
        'version': version,
        'updated_at': updatedAt.toUtc().toIso8601String(),
      };
}

/// One immutable, hash-chained entry of ledger_transactions.
class LedgerTransaction {
  final String txId;

  /// topup | ride_debit | ride_credit_held | escrow_release | refund |
  /// withdrawal_debit | withdrawal_reversal | adjustment_credit |
  /// adjustment_debit | chargeback
  final String type;
  final LedgerMoney amount;
  final LedgerMoney balanceAfter;
  final String prevHash;
  final String txHash;
  final String? description;
  final String? referenceId;
  final String? referenceType;
  final DateTime createdAt;
  final Map<String, dynamic>? metadata;

  const LedgerTransaction({
    required this.txId,
    required this.type,
    required this.amount,
    required this.balanceAfter,
    required this.prevHash,
    required this.txHash,
    this.description,
    this.referenceId,
    this.referenceType,
    required this.createdAt,
    this.metadata,
  });

  factory LedgerTransaction.fromJson(Map<String, dynamic> json) {
    return LedgerTransaction(
      txId: json['tx_id'] as String? ?? '',
      type: json['type'] as String? ?? '',
      amount: LedgerMoney.parse(json['amount_etb'] as String?),
      balanceAfter: LedgerMoney.parse(json['balance_after_etb'] as String?),
      prevHash: json['prev_hash'] as String? ?? '0',
      txHash: json['tx_hash'] as String? ?? '',
      description: json['description'] as String?,
      referenceId: json['reference_id'] as String?,
      referenceType: json['reference_type'] as String?,
      createdAt: DateTime.tryParse(json['created_at'] as String? ?? '') ??
          DateTime.fromMillisecondsSinceEpoch(0),
      metadata: json['metadata'] is Map
          ? Map<String, dynamic>.from(json['metadata'] as Map)
          : null,
    );
  }
}

/// A page of transactions (GET /api/v1/ledger/transactions).
class TransactionPage {
  final List<LedgerTransaction> items;
  final int total;
  final int limit;
  final int offset;

  const TransactionPage({
    required this.items,
    required this.total,
    required this.limit,
    required this.offset,
  });

  bool get hasMore => offset + items.length < total;

  factory TransactionPage.fromJson(Map<String, dynamic> json) {
    final raw = (json['items'] as List? ?? const []);
    return TransactionPage(
      items: raw
          .map((e) =>
              LedgerTransaction.fromJson(Map<String, dynamic>.from(e as Map)))
          .toList(),
      total: (json['total'] as num?)?.toInt() ?? 0,
      limit: (json['limit'] as num?)?.toInt() ?? 50,
      offset: (json['offset'] as num?)?.toInt() ?? 0,
    );
  }
}

/// Response of POST /api/v1/ledger/topup (202 Accepted).
class TopupResult {
  final String topupId;
  final String status; // pending | completed | failed | cancelled
  final LedgerMoney amount;
  final String provider; // telebirr | chapa | mpesa
  final DateTime requestedAt;
  final String note;

  const TopupResult({
    required this.topupId,
    required this.status,
    required this.amount,
    required this.provider,
    required this.requestedAt,
    required this.note,
  });

  factory TopupResult.fromJson(Map<String, dynamic> json) {
    return TopupResult(
      topupId: json['topup_id'] as String? ?? '',
      status: json['status'] as String? ?? 'pending',
      amount: LedgerMoney.parse(json['amount_etb'] as String?),
      provider: json['provider'] as String? ?? '',
      requestedAt: DateTime.tryParse(json['requested_at'] as String? ?? '') ??
          DateTime.now(),
      note: json['note'] as String? ?? '',
    );
  }
}

/// Response of POST /api/v1/ledger/withdrawal (202 Accepted).
class WithdrawalResult {
  final String withdrawalId;
  final String status; // pending | processing | held | completed | failed
  final LedgerMoney amount;
  final LedgerMoney fee;
  final int riskScore;
  final DateTime requestedAt;

  const WithdrawalResult({
    required this.withdrawalId,
    required this.status,
    required this.amount,
    required this.fee,
    required this.riskScore,
    required this.requestedAt,
  });

  factory WithdrawalResult.fromJson(Map<String, dynamic> json) {
    return WithdrawalResult(
      withdrawalId: json['withdrawal_id'] as String? ?? '',
      status: json['status'] as String? ?? 'pending',
      amount: LedgerMoney.parse(json['amount_etb'] as String?),
      fee: LedgerMoney.parse(json['fee_etb'] as String?),
      riskScore: (json['risk_score'] as num?)?.toInt() ?? 0,
      requestedAt: DateTime.tryParse(json['requested_at'] as String? ?? '') ??
          DateTime.now(),
    );
  }
}

/// Response of POST /api/v1/ledger/disputes (200/202).
class DisputeResult {
  final String disputeId;
  final String status; // open | admin_review | resolved_rider | resolved_driver ...
  final DateTime createdAt;

  const DisputeResult({
    required this.disputeId,
    required this.status,
    required this.createdAt,
  });

  factory DisputeResult.fromJson(Map<String, dynamic> json) {
    return DisputeResult(
      disputeId: json['dispute_id'] as String? ?? '',
      status: json['status'] as String? ?? 'open',
      createdAt: DateTime.tryParse(json['created_at'] as String? ?? '') ??
          DateTime.now(),
    );
  }
}

/// Payload of GET /api/v1/ledger/audit/report — signed proof of balance.
class AuditReport {
  final String generatedAt;
  final String userId;
  final LedgerBalance balance;
  final int chainLength;
  final String chainTipHash;
  final bool chainVerified;
  final String signatureAlg;
  final String signature;

  const AuditReport({
    required this.generatedAt,
    required this.userId,
    required this.balance,
    required this.chainLength,
    required this.chainTipHash,
    required this.chainVerified,
    required this.signatureAlg,
    required this.signature,
  });

  bool get isSigned => signature.isNotEmpty;

  factory AuditReport.fromJson(Map<String, dynamic> json) {
    return AuditReport(
      generatedAt: json['generated_at'] as String? ?? '',
      userId: json['user_id'] as String? ?? '',
      balance: LedgerBalance.fromJson(
          Map<String, dynamic>.from(json['balance'] as Map? ?? const {})),
      chainLength: (json['chain_length'] as num?)?.toInt() ?? 0,
      chainTipHash: json['chain_tip_hash'] as String? ?? '',
      chainVerified: json['chain_verified'] as bool? ?? false,
      signatureAlg: json['signature_alg'] as String? ?? '',
      signature: json['signature'] as String? ?? '',
    );
  }

  /// Deterministic canonical map used by the server-side HMAC signature:
  /// every field except signature/signature_alg, in declaration order.
  /// Kept in sync with Report.CanonicalBytes() in report_signer.go.
  Map<String, dynamic> canonicalJson() => {
        'generated_at': generatedAt,
        'user_id': userId,
        'balance': balance.toJson(),
        'chain_length': chainLength,
        'chain_tip_hash': chainTipHash,
        'chain_verified': chainVerified,
      };
}
