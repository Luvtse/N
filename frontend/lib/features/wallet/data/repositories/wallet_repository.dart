import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';

import '../../../../core/network/api_client.dart';
import '../../domain/entities/ledger_models.dart';

// ============================================================================
// SUPPORTED ETHIOPIAN PAYMENT RAILS (Phase E)
// ============================================================================

/// Fiat on-ramp / payout providers wired in
/// `backend/internal/shared/integrations/payments/`. The backend applies its
/// own fallback priority when a rail fails; the client only expresses a
/// preference here.
enum PaymentProvider {
  telebirr('telebirr', 'Telebirr', 'Ethio Telecom mobile money'),
  chapa('chapa', 'Chapa', 'Card / bank / Chapa checkout link'),
  mpesa('mpesa', 'M-Pesa Ethiopia', 'Safaricom STK Push');

  final String apiValue;
  final String label;
  final String description;

  const PaymentProvider(this.apiValue, this.label, this.description);

  static PaymentProvider? tryParse(String raw) {
    for (final p in values) {
      if (p.apiValue == raw.toLowerCase().trim()) return p;
    }
    return null;
  }
}

/// Withdrawal destination types accepted by POST /withdrawal.
enum PayoutDestination {
  bankTransfer('bank_transfer', 'Bank transfer'),
  mpesa('mpesa', 'M-Pesa Ethiopia'),
  telebirrMerchant('telebirr_merchant', 'Telebirr merchant payout');

  final String apiValue;
  final String label;

  const PayoutDestination(this.apiValue, this.label);
}

// ============================================================================
// ABSTRACT INTERFACE
// ============================================================================

/// Wallet / ledger repository — thin typed wrapper over the Phase D HTTP API.
abstract class WalletRepository {
  /// GET /api/v1/ledger/balance
  Future<LedgerBalance> getBalance();

  /// GET /api/v1/ledger/transactions?limit=&offset=
  Future<TransactionPage> listTransactions({int limit = 50, int offset = 0});

  /// POST /api/v1/ledger/topup — optimistic credit + async provider capture.
  Future<TopupResult> requestTopup({
    required LedgerMoney amount,
    required PaymentProvider provider,
    required String idempotencyKey,
    Map<String, dynamic>? metadata,
  });

  /// POST /api/v1/ledger/withdrawal
  Future<WithdrawalResult> requestWithdrawal({
    required LedgerMoney amount,
    required LedgerMoney fee,
    required PayoutDestination destination,
    required Map<String, dynamic> destinationDetails,
    required String idempotencyKey,
  });

  /// POST /api/v1/ledger/disputes
  Future<DisputeResult> fileDispute({
    required String rideId,
    required String reasonCode,
    required String description,
    List<String> evidenceUrls,
  });

  /// GET /api/v1/ledger/audit/report — signed proof of balance.
  Future<AuditReport> fetchAuditReport();
}

// ============================================================================
// IMPLEMENTATION
// ============================================================================

class WalletRepositoryImpl implements WalletRepository {
  static const String _base = '/api/v1/ledger';

  final ApiClient _apiClient;

  WalletRepositoryImpl({required ApiClient apiClient}) : _apiClient = apiClient;

  @override
  Future<LedgerBalance> getBalance() async {
    final response = await _apiClient.get<Map<String, dynamic>>('$_base/balance');
    return LedgerBalance.fromJson(_json(response));
  }

  @override
  Future<TransactionPage> listTransactions(
      {int limit = 50, int offset = 0}) async {
    final response = await _apiClient.get<Map<String, dynamic>>(
      '$_base/transactions',
      queryParameters: {'limit': limit, 'offset': offset},
    );
    return TransactionPage.fromJson(_json(response));
  }

  @override
  Future<TopupResult> requestTopup({
    required LedgerMoney amount,
    required PaymentProvider provider,
    required String idempotencyKey,
    Map<String, dynamic>? metadata,
  }) async {
    final response = await _apiClient.post<Map<String, dynamic>>(
      '$_base/topup',
      data: {
        'amount_etb': amount.toString(),
        'provider': provider.apiValue,
        'idempotency_key': idempotencyKey,
        if (metadata != null && metadata.isNotEmpty) 'metadata': metadata,
      },
    );
    return TopupResult.fromJson(_json(response));
  }

  @override
  Future<WithdrawalResult> requestWithdrawal({
    required LedgerMoney amount,
    required LedgerMoney fee,
    required PayoutDestination destination,
    required Map<String, dynamic> destinationDetails,
    required String idempotencyKey,
  }) async {
    final response = await _apiClient.post<Map<String, dynamic>>(
      '$_base/withdrawal',
      data: {
        'amount_etb': amount.toString(),
        'fee_etb': fee.toString(),
        'destination_type': destination.apiValue,
        'destination_details': destinationDetails,
        'idempotency_key': idempotencyKey,
      },
    );
    return WithdrawalResult.fromJson(_json(response));
  }

  @override
  Future<DisputeResult> fileDispute({
    required String rideId,
    required String reasonCode,
    required String description,
    List<String> evidenceUrls = const [],
  }) async {
    final response = await _apiClient.post<Map<String, dynamic>>(
      '$_base/disputes',
      data: {
        'ride_id': rideId,
        'reason_code': reasonCode,
        'description': description,
        if (evidenceUrls.isNotEmpty) 'evidence_urls': evidenceUrls,
      },
    );
    return DisputeResult.fromJson(_json(response));
  }

  @override
  Future<AuditReport> fetchAuditReport() async {
    final response =
        await _apiClient.get<Map<String, dynamic>>('$_base/audit/report');
    return AuditReport.fromJson(_json(response));
  }

  Map<String, dynamic> _json(Response<dynamic> response) {
    final data = response.data;
    if (data is Map<String, dynamic>) return data;
    debugPrint('wallet: unexpected payload type ${data.runtimeType}');
    return <String, dynamic>{};
  }
}
