// §2.3 Pre-selfie verification — client repository (rider redeem + driver upload).
//
// Talks to the backend selfie module (kill-switch: FEATURE_PRE_SELFIE).
// Endpoints, all under an authenticated ApiClient:
//   POST /api/v1/driver/rides/{rideId}/pre-selfie  multipart field "selfie"
//        -> 201 { id, state: "uploaded", expires_at }
//        -> 403 not the assigned driver | 413 image exceeds 200 KB cap
//           | 415 unsupported content type | 503 storage unavailable
//   GET  /api/v1/selfies/{id}?exp={unix}&sig={hex}  -> raw image bytes
//        The exp/sig pair is minted server-side and delivered on the ride DTO
//        as pre_selfie.redeem_path; clients never parse or forge URLs.
//
// Honesty rules (CONTRIBUTING-RIDE-PARITY Global Rule 2): a missing, expired,
// or failed selfie always maps to one of the four explicit PreSelfieState
// values. We never fabricate a URL, never show a placeholder photo, and never
// conflate "the driver skipped it" with "our stack failed".
import 'dart:typed_data';

import 'package:dio/dio.dart';

import '../../../../core/network/api_client.dart';

// ============================================================================
// MODELS
// ============================================================================

enum PreSelfieState { available, notSent, expired, unavailable, unknown }

PreSelfieState preSelfieStateFrom(String? raw) {
  switch (raw) {
    case 'available':
      return PreSelfieState.available;
    case 'not_sent':
      return PreSelfieState.notSent;
    case 'expired':
      return PreSelfieState.expired;
    case 'unavailable':
      return PreSelfieState.unavailable;
    default:
      return PreSelfieState.unknown;
  }
}

/// Mirrors handlers.PreSelfieInfo JSON exactly (state, driver_pre_selfie_url,
/// redeem_path, expires_at). When the feature flag is off server-side the
/// field is omitted entirely and [PreSelfieInfo.absent] is used, which the UI
/// renders as the honest "verification unavailable" state.
class PreSelfieInfo {
  final PreSelfieState state;

  /// Display URL for debug tooling only — image fetching uses [redeemPath].
  final String url;

  /// Server-minted API path carrying the signature + expiry query params,
  /// e.g. `/api/v1/selfies/<id>?exp=<unix>&sig=<hex>`. Empty unless
  /// state == available.
  final String redeemPath;
  final DateTime? expiresAtUtc;

  const PreSelfieInfo({
    required this.state,
    required this.url,
    required this.redeemPath,
    required this.expiresAtUtc,
  });

  /// The state used when the backend omits `pre_selfie` from the DTO (flag
  /// off / attacher unwired). Distinct from `not_sent`: nothing was skipped
  /// by the driver, the feature simply is not serving this ride.
  static const PreSelfieInfo absent =
      PreSelfieInfo(state: PreSelfieState.unavailable, url: '', redeemPath: '', expiresAtUtc: null);

  bool get isAvailable =>
      state == PreSelfieState.available && redeemPath.isNotEmpty;

  factory PreSelfieInfo.fromJson(Map<String, dynamic> json) {
    return PreSelfieInfo(
      state: preSelfieStateFrom(json['state'] as String?),
      url: json['driver_pre_selfie_url'] as String? ?? '',
      redeemPath: json['redeem_path'] as String? ?? '',
      expiresAtUtc: json['expires_at'] != null
          ? DateTime.tryParse(json['expires_at'] as String)?.toUtc()
          : null,
    );
  }
}

/// Result of a driver-side upload attempt.
class PreSelfieUploadResult {
  final String selfieId;
  final DateTime? expiresAtUtc;

  const PreSelfieUploadResult({required this.selfieId, required this.expiresAtUtc});
}

// ============================================================================
// ERRORS
// ============================================================================

sealed class PreSelfieException implements Exception {
  const PreSelfieException();
}

/// The signed link expired between DTO delivery and tap. UI shows "Selfie
/// expired — ask your driver to resend" (or re-polls the ride).
class PreSelfieLinkExpiredException extends PreSelfieException {
  const PreSelfieLinkExpiredException();
}

/// Storage / signing failure on the server (503). UI shows retry affordance.
class PreSelfieStorageUnavailableException extends PreSelfieException {
  const PreSelfieStorageUnavailableException();
}

/// Network or unexpected status. Carries the raw status for logging; the UI
/// still must not invent success.
class PreSelfieNetworkException extends PreSelfieException {
  final int? statusCode;
  const PreSelfieNetworkException(this.statusCode);
}

/// Driver-side rejection: not the assigned driver (403), oversize (413),
/// bad type (415), or validation (400). [serverMessage] is the backend's own
/// human-readable reason — displayed verbatim, never rewritten into a lie.
class PreSelfieUploadRejectedException extends PreSelfieException {
  final int statusCode;
  final String serverMessage;
  const PreSelfieUploadRejectedException(this.statusCode, this.serverMessage);
}

// ============================================================================
// REPOSITORY
// ============================================================================

abstract class PreSelfieRepository {
  /// Fetch the raw selfie bytes through the server-minted redeem path.
  /// Throws typed exceptions for expired/unavailable/network states.
  Future<Uint8List> fetchSelfieBytes(String redeemPath);

  /// Driver-app upload. [jpegBytes] MUST already be ≤200 KB (enforced here
  /// before any network call so users get instant feedback instead of a
  /// server round-trip rejection).
  Future<PreSelfieUploadResult> uploadDriverSelfie({
    required String rideId,
    required Uint8List jpegBytes,
  });
}

class PreSelfieRepositoryImpl implements PreSelfieRepository {
  static const int maxImageBytes = 200 * 1024;

  final ApiClient _api;

  PreSelfieRepositoryImpl({required ApiClient api}) : _api = api;

  @override
  Future<Uint8List> fetchSelfieBytes(String redeemPath) async {
    // The selfie endpoints use validateStatus<500 semantics (see ApiClient):
    // 4xx responses arrive as normal Responses rather than DioExceptions, so
    // every honest state must be mapped from statusCode directly.
    try {
      final res = await _api.get<List<int>>(
        redeemPath,
        options: Options(responseType: ResponseType.bytes),
      );
      final status = res.statusCode ?? 0;
      if (status == 403 || status == 410) {
        // Expired signature or past expiry — distinct from storage failure.
        throw const PreSelfieLinkExpiredException();
      }
      if (status == 503) {
        throw const PreSelfieStorageUnavailableException();
      }
      if (status != 200 || res.data == null) {
        throw PreSelfieNetworkException(status);
      }
      return Uint8List.fromList(res.data!);
    } on PreSelfieException {
      rethrow;
    } on ApiException catch (e) {
      // ApiClient maps 4xx/5xx to typed exceptions carrying statusCode.
      final status = e.statusCode ?? 0;
      if (status == 403 || status == 410) {
        throw const PreSelfieLinkExpiredException();
      }
      if (status == 503 || e is ServerException) {
        throw const PreSelfieStorageUnavailableException();
      }
      throw PreSelfieNetworkException(status);
    } on DioException catch (e) {
      // Transport-level failure (timeout / connection error). Anything
      // reaching us here is a network fault, never fake success.
      throw PreSelfieNetworkException(e.response?.statusCode);
    }
  }

  @override
  Future<PreSelfieUploadResult> uploadDriverSelfie({
    required String rideId,
    required Uint8List jpegBytes,
  }) async {
    if (jpegBytes.length > maxImageBytes) {
      // Client-side guard mirrors the server cap (defense in depth): fail
      // fast with the same semantics rather than burning a round trip.
      throw const PreSelfieUploadRejectedException(
        413,
        'Selfie exceeds the 200 KB limit even after compression. Retake in better lighting.',
      );
    }
    try {
      final form = FormData.fromMap({
        'selfie': MultipartFile.fromBytes(
          jpegBytes,
          filename: 'pre-selfie.jpg',
          contentType: DioMediaType('image', 'jpeg'),
        ),
      });
      final res = await _api.post<Map<String, dynamic>>(
        '/api/v1/driver/rides/$rideId/pre-selfie',
        data: form,
      );
      final status = res.statusCode ?? 0;
      if (status == 201 || status == 200) {
        final body = res.data ?? const <String, dynamic>{};
        return PreSelfieUploadResult(
          selfieId: body['id'] as String? ?? '',
          expiresAtUtc: body['expires_at'] != null
              ? DateTime.tryParse(body['expires_at'] as String)?.toUtc()
              : null,
        );
      }
      // Defensive: if validateStatus let a non-2xx through as a Response,
      // surface the server's own message verbatim.
      final body = res.data;
      final msg = body != null ? (body['error'] ?? body['message'])?.toString() : null;
      throw PreSelfieUploadRejectedException(
        status,
        msg ?? 'Upload rejected (HTTP $status).',
      );
    } on PreSelfieException {
      rethrow;
    } on ApiException catch (e) {
      // 400/401/403/413/415/503 land here via ApiClient's typed mapping.
      // The backend message is already human-readable — display it verbatim,
      // never rewritten into a generic "something went wrong".
      throw PreSelfieUploadRejectedException(
        e.statusCode ?? 0,
        e.message.isNotEmpty ? e.message : 'Upload failed.',
      );
    } on DioException catch (e) {
      throw PreSelfieUploadRejectedException(
        e.response?.statusCode ?? 0,
        'Upload failed — network error.',
      );
    }
  }
}
