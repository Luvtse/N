import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';

import '../network/websocket_client.dart';
import 'fcm_service.dart';
import 'notification_models.dart';
import 'notification_repository.dart';

/// Central in-app notification hub (§1.4). Merges three sources into one
/// stream the UI can trust:
///
///   1. the WS topic `notification.user.{userId}` (real-time, always-on),
///   2. FCM foreground messages (redundant path when WS is down),
///   3. the paginated HTTP inbox (durable history + unread count).
///
/// Deduplication is by notification id; a row that arrives over both WS and
/// an inbox refresh surfaces exactly once. Unread state is optimistic locally
/// and reconciled with POST /notifications/:id/read.
class NotificationService {
  static const String wsTopicPrefix = 'notification.user.';

  final NotificationRepository repository;
  final WebSocketClient webSocket;
  final FcmService? fcm;

  final _bannerController = StreamController<AppNotification>.broadcast();
  final _unreadController = StreamController<int>.broadcast();

  StreamSubscription<WebSocketMessage>? _wsSub;
  StreamSubscription<AppNotification>? _fcmSub;

  String? _userId;
  final Set<String> _seenIds = {};
  int _unreadCount = 0;

  /// Notifications to surface as the slide-down banner (WS + FCM foreground).
  Stream<AppNotification> get bannerStream => _bannerController.stream;

  /// Live unread count for the app-bar bell badge.
  Stream<int> get unreadStream => _unreadController.stream;
  int get unreadCount => _unreadCount;

  NotificationService({
    required this.repository,
    required this.webSocket,
    this.fcm,
  });

  /// Binds the current user. Called on login/app start; pass null on logout.
  void bindUser(String? userId) {
    if (_userId == userId) return;
    _teardownBindings();
    _userId = userId;
    if (userId == null) {
      _seenIds.clear();
      _setUnread(0);
      return;
    }
    final topic = '$wsTopicPrefix$userId';
    webSocket.subscribe(topic, _onWsCallback);

    _fcmSub = fcm?.foregroundMessages.listen(_ingest);

    unawaited(refreshInbox());
  }

  /// The WebSocketClient dispatches per-topic callbacks with the message's
  /// data map; unwrap either a full notification JSON or an envelope that
  /// carries it under `payload`, and ingest.
  void _onWsCallback(dynamic data) {
    try {
      Map<String, dynamic>? candidate;
      if (data is String) {
        final decoded = jsonDecode(data);
        if (decoded is Map<String, dynamic>) candidate = decoded;
      } else if (data is Map<String, dynamic>) {
        candidate = data;
      }
      if (candidate == null) return;
      final payload = candidate['payload'] ?? candidate;
      if (payload is Map<String, dynamic>) {
        _ingest(AppNotification.fromJson(payload));
      }
    } catch (e) {
      debugPrint('[notif-service] unparseable ws payload ignored: $e');
    }
  }

  void _ingest(AppNotification n) {
    if (n.id.isEmpty) return; // nothing honest to dedupe against
    if (_seenIds.add(n.id)) {
      if (n.isUnread) _setUnread(_unreadCount + 1);
      _bannerController.add(n);
    }
  }

  /// Pulls the first inbox page, seeds the seen-id set and unread count.
  /// Throws so callers (the inbox page) render a retry state — never fake rows.
  Future<void> refreshInbox() async {
    if (_userId == null) return;
    final page = await repository.fetchInbox();
    var unread = 0;
    for (final n in page.items) {
      _seenIds.add(n.id);
      if (n.isUnread) unread++;
    }
    _setUnread(unread);
  }

  /// Marks read optimistically, then server-side; on server failure the local
  /// count is rolled back so the badge never lies about what's persisted.
  Future<bool> markRead(String notificationId) async {
    final wasSeenUnread = _seenIds.contains(notificationId);
    if (wasSeenUnread) _setUnread((_unreadCount - 1).clamp(0, 1 << 30));
    try {
      await repository.markRead(notificationId);
      return true;
    } on Object catch (e) {
      if (wasSeenUnread) _setUnread(_unreadCount + 1);
      debugPrint('[notif-service] markRead failed: $e');
      return false;
    }
  }

  void _setUnread(int value) {
    if (_unreadCount == value) return;
    _unreadCount = value;
    _unreadController.add(value);
  }

  void _teardownBindings() {
    if (_userId != null) {
      webSocket.unsubscribe('$wsTopicPrefix$_userId');
    }
    _wsSub?.cancel();
    _fcmSub?.cancel();
    _wsSub = null;
    _fcmSub = null;
  }

  Future<void> dispose() async {
    _teardownBindings();
    await _bannerController.close();
    await _unreadController.close();
  }
}
