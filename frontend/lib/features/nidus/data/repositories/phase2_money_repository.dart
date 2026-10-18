import 'package:flutter/foundation.dart';

import '../../../../core/network/api_client.dart';

/// Client surface for the Phase 2 money endpoints that ride_repository.dart
/// does not own: the §3.1 payment-method registry, the §3.1 cash-collection
/// confirmation, and the §3.2 promotions (redeem projection + my codes).
///
/// Every method returns a typed result — never a bare throw at call sites —
/// so widgets can render honest "unavailable / retry" states instead of fake
/// data (Global Rule: no placeholders in shipped surfaces).
class Phase2MoneyRepository {
  final ApiClient _apiClient;

  const Phase2MoneyRepository({required ApiClient apiClient})
      : _apiClient = apiClient;

  // ==========================================================================
  // §3.1 Payment-method registry
  //   GET    /api/v1/nidus/payment-methods
  //   POST   /api/v1/nidus/payment-methods
  //   DELETE /api/v1/nidus/payment-methods/{id}
  //   PATCH  /api/v1/nidus/payment-methods/{id}/default
  // ==========================================================================

  /// Returns the user's saved methods. [failure] carries the server message
  /// when the registry is unwired (404) or unreachable — the selector then
  /// shows its "payment methods unavailable" state with retry.
  Future<PaymentMethodListResult> listPaymentMethods() async {
    try {
      final response = await _apiClient.get<Map<String, dynamic>>(
        '/api/v1/nidus/payment-methods',
      );
      final body = response.data;
      if (body == null) {
        return const PaymentMethodListResult.failure(
          message: 'Server returned an empty response',
          code: 'EMPTY_RESPONSE',
        );
      }
      // Backend envelope: {"payment_methods": [...]} — key must match the
      // handler exactly, otherwise the list silently renders empty.
      var items = body['payment_methods'];
      if (items == null && body['data'] is Map<String, dynamic>) {
        items =
            (body['data'] as Map<String, dynamic>)['payment_methods'];
      }
      final list = <SavedPaymentMethod>[];
      if (items is List) {
        for (final raw in items) {
          if (raw is Map<String, dynamic>) {
            list.add(SavedPaymentMethod.fromJson(raw));
          }
        }
      }
      return PaymentMethodListResult.success(list);
    } on ApiException catch (e) {
      return PaymentMethodListResult.failure(message: e.message, code: e.errorCode);
    } catch (e) {
      return PaymentMethodListResult.failure(
        message: 'Failed to load payment methods: $e',
        code: 'UNKNOWN_ERROR',
      );
    }
  }

  /// Adds a method. [type] must be one of wallet | card | cash. Card metadata
  /// (last4/brand/expiry) comes from the PCI-compliant tokenization SDK — this
  /// client never collects or sends a PAN (Global Rule: never roll your own
  /// card capture). Cash needs no metadata.
  Future<PaymentOpResult> addPaymentMethod({
    required String type,
    String? last4,
    String? brand,
    int? expMonth,
    int? expYear,
    bool makeDefault = false,
  }) async {
    try {
      final response = await _apiClient.post<Map<String, dynamic>>(
        '/api/v1/nidus/payment-methods',
        data: {
          'type': type,
          if (last4 != null) 'last4': last4,
          if (brand != null) 'brand': brand,
          if (expMonth != null) 'exp_month': expMonth,
          if (expYear != null) 'exp_year': expYear,
          'make_default': makeDefault,
        },
      );
      final body = response.data;
      final id = body?['id']?.toString();
      if (id == null || id.isEmpty) {
        return const PaymentOpResult.failure(
          message: 'Payment method was created but no id was returned',
          code: 'INVALID_RESPONSE',
        );
      }
      return PaymentOpResult.success(id: id);
    } on ApiException catch (e) {
      return PaymentOpResult.failure(message: e.message, code: e.errorCode);
    } catch (e) {
      return PaymentOpResult.failure(
        message: 'Failed to add payment method: $e',
        code: 'UNKNOWN_ERROR',
      );
    }
  }

  Future<PaymentOpResult> deletePaymentMethod(String id) async {
    try {
      await _apiClient.delete<Map<String, dynamic>>(
        '/api/v1/nidus/payment-methods/$id',
      );
      return PaymentOpResult.success(id: id);
    } on ApiException catch (e) {
      return PaymentOpResult.failure(message: e.message, code: e.errorCode);
    } catch (e) {
      return PaymentOpResult.failure(
        message: 'Failed to remove payment method: $e',
        code: 'UNKNOWN_ERROR',
      );
    }
  }

  Future<PaymentOpResult> setDefaultPaymentMethod(String id) async {
    try {
      await _apiClient.patch<Map<String, dynamic>>(
        '/api/v1/nidus/payment-methods/$id/default',
      );
      return PaymentOpResult.success(id: id);
    } on ApiException catch (e) {
      return PaymentOpResult.failure(message: e.message, code: e.errorCode);
    } catch (e) {
      return PaymentOpResult.failure(
        message: 'Failed to update default: $e',
        code: 'UNKNOWN_ERROR',
      );
    }
  }

  // ==========================================================================
  // §3.1 Cash collection confirmation (driver-side)
  //   POST /api/v1/nidus/rides/{rideId}/confirm-cash-collection
  // ==========================================================================

  Future<CashConfirmResult> confirmCashCollection(String rideId) async {
    try {
      final response = await _apiClient.post<Map<String, dynamic>>(
        '/api/v1/nidus/rides/$rideId/confirm-cash-collection',
      );
      final body = response.data;
      final status = body?['status']?.toString();
      return CashConfirmResult.success(status: status ?? 'paid');
    } on ApiException catch (e) {
      // NOT_A_CASH_RIDE / RIDE_NOT_ELIGIBLE are terminal business answers,
      // not transport failures — the UI renders them as explanations.
      return CashConfirmResult.failure(message: e.message, code: e.errorCode);
    } catch (e) {
      return CashConfirmResult.failure(
        message: 'Could not confirm collection: $e',
        code: 'UNKNOWN_ERROR',
      );
    }
  }

  // ==========================================================================
  // §3.2 Promotions
  //   POST /api/v1/promotions/redeem  (pre-ride validation + projection)
  //   GET  /api/v1/promotions/mine    (active codes for this user)
  // ==========================================================================

  Future<PromoRedeemResult> redeemPromo({
    required String code,
    required double fare,
    required String currency,
    String? rideType,
  }) async {
    try {
      final response = await _apiClient.post<Map<String, dynamic>>(
        '/api/v1/promotions/redeem',
        data: {
          'code': code,
          'fare': fare,
          'currency': currency,
          if (rideType != null) 'ride_type': rideType,
        },
      );
      final body = response.data;
      final discount = body?['projected_discount'];
      if (body == null || discount is! num) {
        return const PromoRedeemResult.failure(
          message: 'Unexpected redeem response shape',
          code: 'INVALID_RESPONSE',
        );
      }
      return PromoRedeemResult.success(PromoProjection(
        code: body['code'] as String? ?? code,
        type: body['type'] as String? ?? '',
        value: (body['value'] as num?)?.toDouble() ?? 0,
        projectedDiscount: discount.toDouble(),
        currency: body['currency'] as String? ?? currency,
      ));
    } on ApiException catch (e) {
      return PromoRedeemResult.failure(message: e.message, code: e.errorCode);
    } catch (e) {
      return PromoRedeemResult.failure(
        message: 'Promo check failed: $e',
        code: 'UNKNOWN_ERROR',
      );
    }
  }

  Future<PromoListResult> listMyPromos() async {
    try {
      final response = await _apiClient.get<Map<String, dynamic>>(
        '/api/v1/promotions/mine',
      );
      final body = response.data;
      // Backend envelope: {"promotions": [...]} (GET /promotions/mine).
      var items = body?['promotions'];
      if (items == null && body?['data'] is Map<String, dynamic>) {
        items = (body!['data'] as Map<String, dynamic>)['promotions'];
      }
      final list = <PromoCodeInfo>[];
      if (items is List) {
        for (final raw in items) {
          if (raw is Map<String, dynamic>) {
            list.add(PromoCodeInfo.fromJson(raw));
          }
        }
      }
      return PromoListResult.success(list);
    } on ApiException catch (e) {
      return PromoListResult.failure(message: e.message, code: e.errorCode);
    } catch (e) {
      return PromoListResult.failure(
        message: 'Failed to load promo codes: $e',
        code: 'UNKNOWN_ERROR',
      );
    }
  }

  // ==========================================================================
  // §3.4 Email receipt
  //   POST /api/v1/nidus/rides/{rideId}/email-receipt
  // The backend renders the itemized HTML from its own authoritative ride
  // record and dispatches through the notification module's email adapter —
  // the client never sends amounts, only which ride to email.
  // ==========================================================================

  Future<EmailReceiptResult> emailReceipt(String rideId) async {
    try {
      final response = await _apiClient.post<Map<String, dynamic>>(
        '/api/v1/nidus/rides/$rideId/email-receipt',
      );
      // Backend answers 202 Accepted with {"status": "queued", ...}. Queued
      // is the honest success signal — delivery happens asynchronously via
      // the notification dispatcher, so we never claim "sent".
      final status = response.data?['status']?.toString();
      if (status != 'queued') {
        return const EmailReceiptResult.failure(
          message: 'The server did not accept the receipt for delivery.',
          code: 'INVALID_RESPONSE',
        );
      }
      return const EmailReceiptResult.success();
    } on ApiException catch (e) {
      // RECEIPT_EMAIL_UNAVAILABLE / RIDE_NOT_COMPLETED are honest terminal
      // answers; anything else is transport and shows retry.
      return EmailReceiptResult.failure(message: e.message, code: e.errorCode);
    } catch (e) {
      return EmailReceiptResult.failure(
        message: 'Could not send the receipt email: $e',
        code: 'UNKNOWN_ERROR',
      );
    }
  }
}

// ============================================================================
// Models
// ============================================================================

/// One row of GET /nidus/payment-methods. Mirrors the backend DTO exactly:
/// id/type/is_default/label/last4/brand/exp_month/exp_year/created_at.
class SavedPaymentMethod {
  final String id;
  final String type; // wallet | card | cash
  final bool isDefault;
  final String label;
  final String? last4;
  final String? brand;
  final int? expMonth;
  final int? expYear;

  const SavedPaymentMethod({
    required this.id,
    required this.type,
    required this.isDefault,
    required this.label,
    this.last4,
    this.brand,
    this.expMonth,
    this.expYear,
  });

  bool get isCash => type == 'cash';

  factory SavedPaymentMethod.fromJson(Map<String, dynamic> json) {
    return SavedPaymentMethod(
      id: json['id'].toString(),
      type: json['type'] as String? ?? '',
      isDefault: json['is_default'] as bool? ?? false,
      label: json['label'] as String? ?? 'Payment method',
      last4: json['last4'] as String?,
      brand: json['brand'] as String?,
      expMonth: json['exp_month'] as int?,
      expYear: json['exp_year'] as int?,
    );
  }
}

@immutable
class PaymentMethodListResult {
  final List<SavedPaymentMethod> methods;
  final String? failureMessage;
  final String? failureCode;
  final bool ok;

  const PaymentMethodListResult._({
    required this.ok,
    this.methods = const [],
    this.failureMessage,
    this.failureCode,
  });

  const PaymentMethodListResult.success(this.methods)
      : this._(ok: true, methods: methods);

  const PaymentMethodListResult.failure({
    required String message,
    required String code,
  }) : this._(ok: false, failureMessage: message, failureCode: code);
}

@immutable
class PaymentOpResult {
  final bool ok;
  final String? id;
  final String? message;
  final String? code;

  const PaymentOpResult._({this.ok = false, this.id, this.message, this.code});

  const PaymentOpResult.success({required String id})
      : this._(ok: true, id: id);

  const PaymentOpResult.failure({required String message, required String code})
      : this._(message: message, code: code);
}

@immutable
class CashConfirmResult {
  final bool ok;
  final String? status;
  final String? message;
  final String? code;

  const CashConfirmResult._({this.ok = false, this.status, this.message, this.code});

  const CashConfirmResult.success({required String status})
      : this._(ok: true, status: status);

  const CashConfirmResult.failure({required String message, required String code})
      : this._(message: message, code: code);
}

/// Result of POST /nidus/rides/{id}/email-receipt (§3.4). Success means the
/// server QUEUED the durable email copy — never that it already landed in
/// the inbox, which we cannot know from here.
@immutable
class EmailReceiptResult {
  final bool ok;
  final String? message;
  final String? code;

  const EmailReceiptResult._({this.ok = false, this.message, this.code});

  const EmailReceiptResult.success() : this._(ok: true);

  const EmailReceiptResult.failure({required String message, required String code})
      : this._(message: message, code: code);
}

/// Projected discount returned by POST /promotions/redeem before booking.
@immutable
class PromoProjection {
  final String code;
  final String type; // percent | fixed
  final double value;
  final double projectedDiscount;
  final String currency;

  const PromoProjection({
    required this.code,
    required this.type,
    required this.value,
    required this.projectedDiscount,
    required this.currency,
  });
}

/// One catalog entry from GET /promotions/mine.
@immutable
class PromoCodeInfo {
  final String code;
  final String type;
  final double value;
  final double? maxDiscount;
  final double minFare;
  final DateTime? validTo;
  final List<String> applicableRideTypes;

  const PromoCodeInfo({
    required this.code,
    required this.type,
    required this.value,
    this.maxDiscount,
    required this.minFare,
    this.validTo,
    this.applicableRideTypes = const [],
  });

  factory PromoCodeInfo.fromJson(Map<String, dynamic> json) {
    DateTime? validTo;
    final v = json['valid_to'];
    if (v is String) {
      validTo = DateTime.tryParse(v);
    }
    return PromoCodeInfo(
      code: json['code'] as String? ?? '',
      type: json['type'] as String? ?? '',
      value: (json['value'] as num?)?.toDouble() ?? 0,
      maxDiscount: (json['max_discount'] as num?)?.toDouble(),
      minFare: (json['min_fare'] as num?)?.toDouble() ?? 0,
      validTo: validTo,
      applicableRideTypes: (json['applicable_ride_types'] as List?)
              ?.map((e) => e.toString())
              .toList() ??
          const [],
    );
  }
}

@immutable
class PromoRedeemResult {
  final bool ok;
  final PromoProjection? projection;
  final String? message;
  final String? code;

  const PromoRedeemResult._({this.ok = false, this.projection, this.message, this.code});

  const PromoRedeemResult.success(PromoProjection p)
      : this._(ok: true, projection: p);

  const PromoRedeemResult.failure({required String message, required String code})
      : this._(message: message, code: code);
}

@immutable
class PromoListResult {
  final bool ok;
  final List<PromoCodeInfo> codes;
  final String? message;
  final String? code;

  const PromoListResult._({
    this.ok = false,
    this.codes = const [],
    this.message,
    this.code,
  });

  const PromoListResult.success(List<PromoCodeInfo> c)
      : this._(ok: true, codes: c);

  const PromoListResult.failure({required String message, required String code})
      : this._(message: message, code: code);
}
