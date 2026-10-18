import 'package:flutter/foundation.dart';

import '../../../../core/network/api_client.dart';

/// Client surface for the §3.3 Split Fare endpoints:
///   POST /api/v1/nidus/rides/{rideId}/split          (owner creates invites)
///   GET  /api/v1/nidus/rides/{rideId}/split          (owner reads status)
///   POST /api/v1/nidus/split/{inviteToken}/accept    (public, tokenized link)
///
/// Mirrors split_handler.go DTOs exactly (splitInviteDTO / splitStatusDTO).
/// Every method returns a typed result — never a bare throw at call sites —
/// so the split sheet can render honest "unavailable / retry" states instead
/// of fake numbers (Global Rule: no placeholders in shipped surfaces).
class SplitFareRepository {
  final ApiClient _apiClient;

  const SplitFareRepository({required ApiClient apiClient})
      : _apiClient = apiClient;

  /// Creates split invites for a completed ride. [shares] is the list of
  /// (contact, shareAmount) pairs; the server reduces the inviter's fare by
  /// the sum of ACCEPTED shares only — creating invites does not change the
  /// inviter's charge until invitees accept and pay.
  Future<SplitStatusResult> createSplit(
    String rideId,
    List<SplitShareRequest> shares,
  ) async {
    try {
      final response = await _apiClient.post<Map<String, dynamic>>(
        '/api/v1/nidus/rides/$rideId/split',
        data: {
          'shares': shares
              .map((s) => {
                    'contact': s.contact,
                    'share_amount': s.shareAmount,
                  })
              .toList(),
        },
      );
      final body = response.data;
      if (body == null) {
        return const SplitStatusResult.failure(
          message: 'Server returned an empty response',
          code: 'EMPTY_RESPONSE',
        );
      }
      final payload = _unwrap(body);
      if (payload == null) {
        return const SplitStatusResult.failure(
          message: 'Unexpected response shape from split service',
          code: 'BAD_SHAPE',
        );
      }
      return SplitStatusResult.success(SplitStatus.fromJson(payload));
    } on ApiException catch (e) {
      return SplitStatusResult.failure(message: e.message, code: e.errorCode);
    } catch (e) {
      return SplitStatusResult.failure(
        message: 'Could not reach the split service: $e',
        code: 'NETWORK',
      );
    }
  }

  /// Reads the current split state for a ride (invitee statuses, accepted
  /// totals, remaining inviter share). Used to refresh the sheet after each
  /// WS/notification-driven update.
  Future<SplitStatusResult> getSplit(String rideId) async {
    try {
      final response = await _apiClient.get<Map<String, dynamic>>(
        '/api/v1/nidus/rides/$rideId/split',
      );
      final body = response.data;
      if (body == null) {
        return const SplitStatusResult.failure(
          message: 'Server returned an empty response',
          code: 'EMPTY_RESPONSE',
        );
      }
      final payload = _unwrap(body);
      if (payload == null) {
        return const SplitStatusResult.failure(
          message: 'Unexpected response shape from split service',
          code: 'BAD_SHAPE',
        );
      }
      return SplitStatusResult.success(SplitStatus.fromJson(payload));
    } on ApiException catch (e) {
      return SplitStatusResult.failure(message: e.message, code: e.errorCode);
    } catch (e) {
      return SplitStatusResult.failure(
        message: 'Could not reach the split service: $e',
        code: 'NETWORK',
      );
    }
  }

  /// Accepts a split invite via its tokenized link (the recipient side of the
  /// email/SMS invite). Public endpoint — no rider auth required, the token
  /// is the capability.
  Future<SplitOpResult> acceptInvite(String inviteToken) async {
    try {
      final response = await _apiClient.post<Map<String, dynamic>>(
        '/api/v1/nidus/split/$inviteToken/accept',
      );
      final body = response.data;
      if (response.statusCode >= 200 && response.statusCode < 300) {
        return SplitOpResult.success();
      }
      final message = body != null ? _extractMessage(_unwrap(body) ?? body) : null;
      return SplitOpResult.failure(
        message: message ?? 'Server rejected the invite',
        code: 'SERVER_${response.statusCode}',
      );
    } on ApiException catch (e) {
      return SplitOpResult.failure(message: e.message, code: e.errorCode);
    } catch (e) {
      return SplitOpResult.failure(
        message: 'Could not reach the split service: $e',
        code: 'NETWORK',
      );
    }
  }

  static Map<String, dynamic>? _unwrap(Map<String, dynamic> body) {
    if (body.containsKey('ride_id') || body.containsKey('exists')) {
      return body;
    }
    final data = body['data'];
    if (data is Map<String, dynamic>) return data;
    return null;
  }

  static String? _extractMessage(Map<String, dynamic> map) {
    final err = map['error'];
    if (err is Map<String, dynamic>) {
      final m = err['message'];
      if (m is String) return m;
    }
    final m = map['message'];
    return m is String ? m : null;
  }
}

/// One requested share in a create-split call.
@immutable
class SplitShareRequest {
  final String contact; // phone or email, sent as-is to the backend
  final double shareAmount;

  const SplitShareRequest({required this.contact, required this.shareAmount});
}

/// One invitee row, mirroring splitInviteDTO on the Go side.
@immutable
class SplitInviteInfo {
  final String id;
  final String inviteeContact;
  final double shareAmount;
  final String status; // pending | accepted | paid | declined | expired
  final DateTime createdAt;
  final DateTime? acceptedAt;
  final DateTime? paidAt;

  const SplitInviteInfo({
    required this.id,
    required this.inviteeContact,
    required this.shareAmount,
    required this.currencyTag,
    required this.status,
    required this.createdAt,
    this.acceptedAt,
    this.paidAt,
  });

  /// Raw currency string from the DTO (kept as a tag, not parsed to double).
  final String currencyTag;

  factory SplitInviteInfo.fromJson(Map<String, dynamic> json) {
    return SplitInviteInfo(
      id: json['id'] as String? ?? '',
      inviteeContact: json['invitee_contact'] as String? ?? '',
      shareAmount: (json['share_amount'] as num?)?.toDouble() ?? 0,
      currencyTag: json['currency'] as String? ?? '',
      status: json['status'] as String? ?? 'pending',
      createdAt: DateTime.tryParse(json['created_at'] as String? ?? '') ??
          DateTime.now(),
      acceptedAt: DateTime.tryParse(json['accepted_at'] as String? ?? ''),
      paidAt: DateTime.tryParse(json['paid_at'] as String? ?? ''),
    );
  }
}

/// Full split state for a ride, mirroring splitStatusDTO.
@immutable
class SplitStatus {
  final bool exists;
  final String rideId;
  final double fare;
  final String currency;
  final double inviterShare;
  final double totalSplit;
  final double remainingToPay;
  final List<SplitInviteInfo> invitees;

  const SplitStatus({
    required this.exists,
    required this.rideId,
    required this.fare,
    required this.currency,
    required this.inviterShare,
    required this.totalSplit,
    required this.remainingToPay,
    required this.invitees,
  });

  factory SplitStatus.fromJson(Map<String, dynamic> json) {
    final raw = json['invitees'];
    final list = <SplitInviteInfo>[];
    if (raw is List) {
      for (final item in raw) {
        if (item is Map<String, dynamic>) {
          list.add(SplitInviteInfo.fromJson(item));
        }
      }
    }
    return SplitStatus(
      exists: json['exists'] as bool? ?? false,
      rideId: json['ride_id'] as String? ?? '',
      fare: (json['fare'] as num?)?.toDouble() ?? 0,
      currency: json['currency'] as String? ?? '',
      inviterShare: (json['inviter_share'] as num?)?.toDouble() ?? 0,
      totalSplit: (json['total_split'] as num?)?.toDouble() ?? 0,
      remainingToPay: (json['remaining_to_pay'] as num?)?.toDouble() ?? 0,
      invitees: list,
    );
  }
}

@immutable
class SplitStatusResult {
  final SplitStatus? status;
  final String? failureMessage;
  final String? failureCode;

  const SplitStatusResult._({this.status, this.failureMessage, this.failureCode});

  factory SplitStatusResult.success(SplitStatus status) =>
      SplitStatusResult._(status: status);

  factory SplitStatusResult.failure({
    required String message,
    required String code,
  }) =>
      SplitStatusResult._(failureMessage: message, failureCode: code);

  bool get isSuccess => status != null;
}

@immutable
class SplitOpResult {
  final String? failureMessage;
  final String? failureCode;

  const SplitOpResult._({this.failureMessage, this.failureCode});

  factory SplitOpResult.success() => const SplitOpResult._();

  factory SplitOpResult.failure({
    required String message,
    required String code,
  }) =>
      SplitOpResult._(failureMessage: message, failureCode: code);

  bool get isSuccess => failureMessage == null;
}
