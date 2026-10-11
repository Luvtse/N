import 'dart:io';

import 'package:dio/dio.dart';

import '../network/api_client.dart';
import 'notification_models.dart';

/// Thrown when an inbox/device endpoint fails in a way the UI must surface
/// with a retry affordance rather than fake data (§0 rule: no placeholders).
class NotificationApiException implements Exception {
  final String message;
  final int? statusCode;

  const NotificationApiException(this.message, {this.statusCode});

  @override
  String toString() =>
      'NotificationApiException($statusCode): $message';
}

/// Flutter client for the notifications module endpoints defined in §1.4:
///
///   POST   /notifications/devices            register/upsert FCM token
///   DELETE /notifications/devices/:token     revoke on logout
///   GET    /notifications/inbox?cursor=      paginated in-app inbox
///   POST   /notifications/:id/read           mark one row read
///
/// Every method throws [NotificationApiException] on failure — callers render
/// an honest error state with retry, never fabricated rows.
class NotificationRepository {
  final ApiClient _api;

  const NotificationRepository({required ApiClient api}) : _api = api;

  /// Registers (upserts) this device's push token with the backend so the
  /// FcmAdapter can reach the user while the app is backgrounded/killed.
  Future<void> registerDeviceToken({
    required String token,
    required String appVersion,
  }) async {
    final platform = Platform.isIOS
        ? 'ios'
        : Platform.isAndroid
            ? 'android'
            : 'other';
    try {
      final resp = await _api.post<dynamic>(
        '/notifications/devices',
        data: {
          'token': token,
          'platform': platform,
          'app_version': appVersion,
        },
      );
      _ensureSuccess(resp, 'register device token');
    } on DioException catch (e) {
      throw NotificationApiException(
        e.message ?? 'network failure registering device token',
        statusCode: e.response?.statusCode,
      );
    }
  }

  /// Revokes the token on logout so pushes stop arriving on a signed-out
  /// install.
  Future<void> revokeDeviceToken(String token) async {
    try {
      final resp = await _api.delete<dynamic>(
        '/notifications/devices/${Uri.encodeComponent(token)}',
      );
      _ensureSuccess(resp, 'revoke device token');
    } on DioException catch (e) {
      throw NotificationApiException(
        e.message ?? 'network failure revoking device token',
        statusCode: e.response?.statusCode,
      );
    }
  }

  /// Fetches one page of the in-app inbox. `cursor` is opaque and comes from
  /// [InboxPage.nextCursor]; null fetches the first (newest) page.
  Future<InboxPage> fetchInbox({String? cursor}) async {
    try {
      final resp = await _api.get<dynamic>(
        '/notifications/inbox',
        queryParameters: {
          if (cursor != null && cursor.isNotEmpty) 'cursor': cursor,
          'limit': 30,
        },
      );
      _ensureSuccess(resp, 'fetch inbox');
      final data = resp.data;
      if (data is Map<String, dynamic>) {
        return InboxPage.fromJson(data);
      }
      throw const NotificationApiException('inbox returned malformed body');
    } on DioException catch (e) {
      throw NotificationApiException(
        e.message ?? 'network failure fetching inbox',
        statusCode: e.response?.statusCode,
      );
    }
  }

  /// Marks a single notification read. Idempotent server-side.
  Future<void> markRead(String notificationId) async {
    try {
      final resp = await _api.post<dynamic>(
        '/notifications/${Uri.encodeComponent(notificationId)}/read',
      );
      _ensureSuccess(resp, 'mark notification read');
    } on DioException catch (e) {
      throw NotificationApiException(
        e.message ?? 'network failure marking read',
        statusCode: e.response?.statusCode,
      );
    }
  }

  void _ensureSuccess(Response resp, String action) {
    final code = resp.statusCode ?? 0;
    if (code < 200 || code >= 300) {
      throw NotificationApiException(
        'failed to $action (HTTP $code)',
        statusCode: code,
      );
    }
  }
}
