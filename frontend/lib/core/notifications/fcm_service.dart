import 'dart:async';
import 'dart:convert';

import 'package:firebase_messaging/firebase_messaging.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_local_notifications/flutter_local_notifications.dart';
import 'package:package_info_plus/package_info_plus.dart';

import 'notification_models.dart';
import 'notification_repository.dart';

/// Android channel used for every local notification we post on behalf of an
/// FCM payload (Android 8+ requires a channel; the id is stable so users can
/// manage it in system settings).
const String kNidusPushChannelId = 'nidus_push';
const String kNidusPushChannelName = 'Nidus notifications';

/// Foreground messages are surfaced as an in-app banner, not a system tray
/// entry; this id is reserved for background/killed pushes we re-post locally.
const int _kLocalNotificationId = 4200;

/// Headless entrypoint for messages arriving while the app is terminated.
/// Must be a top-level function per firebase_messaging's contract. It only
/// records the payload — navigation happens when the user taps and the app
/// resumes via [FcmService] `getInitialMessage` handling.
@pragma('vm:entry-point')
Future<void> nidusFirebaseMessagingBackgroundHandler(
    RemoteMessage message) async {
  // No UI work is possible here. Keep the handler side-effect free beyond
  // letting the OS display the push's notification payload (already handled
  // by FCM itself when a `notification` block is present).
  debugPrint('[fcm] background message received: ${message.messageId}');
}

/// Result of a tap on a notification (banner, tray entry, or push).
class NotificationTap {
  /// Parsed notification when the payload carried one; null when the tap came
  /// from a bare FCM notification without our data block.
  final AppNotification? notification;

  /// Raw deep link to navigate to, if any.
  final String? deepLink;

  const NotificationTap({this.notification, this.deepLink});
}

/// FCM integration per §1.4: permission → token registration → refresh →
/// foreground banner / background tap / killed tap. All failures surface as
/// `available=false` plus a logged reason — never a fake "notifications on"
/// state.
class FcmService {
  final NotificationRepository repository;
  final FirebaseMessaging _messaging;
  final FlutterLocalNotificationsPlugin _localNotifications;

  final StreamController<AppNotification> _foregroundController =
      StreamController<AppNotification>.broadcast();
  final StreamController<NotificationTap> _tapController =
      StreamController<NotificationTap>.broadcast();

  StreamSubscription<RemoteMessage>? _onMessageSub;
  StreamSubscription<RemoteMessage>? _onOpenedSub;
  StreamSubscription<String>? _tokenRefreshSub;

  String? _currentToken;
  bool _permissionGranted = false;
  bool _initialized = false;

  /// Broadcast stream of messages that arrived while the app was in the
  /// foreground — the in-app banner widget subscribes to this (§1.4 step 6).
  Stream<AppNotification> get foregroundMessages =>
      _foregroundController.stream;

  /// Broadcast stream of user taps on any notification surface (in-app banner,
  /// system tray, killed-launch). Consumed by notification_router.dart.
  Stream<NotificationTap> get taps => _tapController.stream;

  bool get permissionGranted => _permissionGranted;
  String? get currentToken => _currentToken;

  FcmService({
    required this.repository,
    FirebaseMessaging? messaging,
    FlutterLocalNotificationsPlugin? localNotifications,
  })  : _messaging = messaging ?? FirebaseMessaging.instance,
        _localNotifications =
            localNotifications ?? FlutterLocalNotificationsPlugin();

  /// Full bootstrap. Safe to call once from the composition root after
  /// Firebase.initializeApp(). Returns false when permission was denied or
  /// token registration failed; callers must then show the honest
  /// "push unavailable" state rather than assuming delivery works.
  Future<bool> initialize() async {
    if (_initialized) return _permissionGranted;
    _initialized = true;

    // iOS + Android 13+ alert/permission prompt.
    NotificationSettings settings;
    try {
      settings = await _messaging.requestPermission(
        alert: true,
        badge: true,
        sound: true,
      );
    } catch (e) {
      debugPrint('[fcm] requestPermission threw: $e');
      return false;
    }
    _permissionGranted = settings.authorizationStatus ==
            AuthorizationStatus.authorized ||
        settings.authorizationStatus == AuthorizationStatus.provisional;

    await _messaging.setBackgroundMessageHandler(
        nidusFirebaseMessagingBackgroundHandler);

    // Local notifications plugin — used to re-post data-only pushes that
    // arrive while the app is in the background but not visible.
    const initSettings = InitializationSettings(
      android: AndroidInitializationSettings('@mipmap/ic_launcher'),
      iOS: DarwinInitializationSettings(
        requestAlertPermission: false,
        requestBadgePermission: false,
        requestSoundPermission: false,
      ),
    );
    await _localNotifications.initialize(
      initSettings,
      onDidReceiveNotificationResponse: (response) {
        _tapController.add(NotificationTap(
          deepLink: response.payload,
        ));
      },
    );

    // Foreground: hand parsed messages to the in-app banner stream.
    _onMessageSub = FirebaseMessaging.onMessage.listen((message) {
      final notification = _parse(message);
      if (notification != null) {
        _foregroundController.add(notification);
      }
    });

    // Background (app alive, not visible): tap → deep link.
    _onOpenedSub = FirebaseMessaging.onMessageOpenedApp.listen((message) {
      final notification = _parse(message);
      _tapController.add(NotificationTap(
        notification: notification,
        deepLink: notification?.deepLink.isNotEmpty == true
            ? notification!.deepLink
            : message.data['deep_link'] as String?,
      ));
    });

    // Killed launch: tap opened the app from a notification.
    final initial = await _messaging.getInitialMessage();
    if (initial != null) {
      final notification = _parse(initial);
      _tapController.add(NotificationTap(
        notification: notification,
        deepLink: notification?.deepLink,
      ));
    }

    // Token: register now, re-register on refresh.
    if (_permissionGranted) {
      try {
        final token = await _messaging.getToken();
        if (token != null) await _registerToken(token);
      } catch (e) {
        debugPrint('[fcm] getToken failed: $e');
        _permissionGranted = false;
      }
      _tokenRefreshSub =
          _messaging.onTokenRefresh.listen(_registerTokenSafe);
    }

    return _permissionGranted;
  }

  Future<void> _registerTokenSafe(String token) async {
    try {
      await _registerToken(token);
    } catch (e) {
      debugPrint('[fcm] token refresh registration failed: $e');
    }
  }

  Future<void> _registerToken(String token) async {
    _currentToken = token;
    final info = await PackageInfo.fromPlatform();
    await repository.registerDeviceToken(
      token: token,
      appVersion: info.version,
    );
  }

  /// Revoke on logout so pushes stop reaching a signed-out device.
  Future<void> unregisterOnLogout() async {
    final token = _currentToken;
    if (token == null) return;
    try {
      await repository.revokeDeviceToken(token);
    } catch (e) {
      debugPrint('[fcm] revoke failed (continuing logout anyway): $e');
    }
    _currentToken = null;
  }

  /// Parses an FCM message into our wire model. Data-only messages carry the
  /// full notification JSON under the `notification` key; messages with a
  /// native notification block fall back to title/body/deep_link data keys.
  AppNotification? _parse(RemoteMessage message) {
    try {
      final raw = message.data['notification'];
      if (raw is String && raw.isNotEmpty) {
        final decoded = jsonDecode(raw);
        if (decoded is Map<String, dynamic>) {
          return AppNotification.fromJson(decoded);
        }
      }
      if (raw is Map<String, dynamic>) {
        return AppNotification.fromJson(raw);
      }
      final notif = message.notification;
      if (notif != null) {
        return AppNotification(
          id: message.messageId ?? '',
          userId: '',
          channel: NotificationChannel.fcm,
          category: notificationCategoryFromWire(
              '${message.data['category'] ?? 'other'}'),
          title: notif.title ?? '',
          body: notif.body ?? '',
          deepLink: '${message.data['deep_link'] ?? ''}',
          payload: message.data,
          createdAt:
              message.sentTime?.toUtc() ?? DateTime.now().toUtc(),
        );
      }
      return null;
    } catch (e) {
      debugPrint('[fcm] failed to parse message: $e');
      return null;
    }
  }

  Future<void> dispose() async {
    await _onMessageSub?.cancel();
    await _onOpenedSub?.cancel();
    await _tokenRefreshSub?.cancel();
    await _foregroundController.close();
    await _tapController.close();
  }
}
