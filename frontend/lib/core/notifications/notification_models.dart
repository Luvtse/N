import 'dart:convert';

/// Notification channels the backend dispatcher can fan out to (§1.4).
/// Mirrors the Go entity `Channel` values: in_app, fcm, email.
enum NotificationChannel { inApp, fcm, email }

NotificationChannel notificationChannelFromWire(String raw) {
  switch (raw) {
    case 'in_app':
      return NotificationChannel.inApp;
    case 'fcm':
      return NotificationChannel.fcm;
    case 'email':
      return NotificationChannel.email;
    default:
      return NotificationChannel.inApp;
  }
}

String notificationChannelToWire(NotificationChannel channel) {
  switch (channel) {
    case NotificationChannel.inApp:
      return 'in_app';
    case NotificationChannel.fcm:
      return 'fcm';
    case NotificationChannel.email:
      return 'email';
  }
}

/// Notification categories carried on every dispatched notification (§1.4).
/// Unknown wire values degrade to [other] so a new backend category never
/// crashes an older client build.
enum NotificationCategory {
  rideMatched,
  driverArriving,
  paymentFailed,
  promo,
  safety,
  receipt,
  scheduledRideReminder,
  other,
}

NotificationCategory notificationCategoryFromWire(String raw) {
  switch (raw) {
    case 'ride_matched':
      return NotificationCategory.rideMatched;
    case 'driver_arriving':
      return NotificationCategory.driverArriving;
    case 'payment_failed':
      return NotificationCategory.paymentFailed;
    case 'promo':
      return NotificationCategory.promo;
    case 'safety':
      return NotificationCategory.safety;
    case 'receipt':
      return NotificationCategory.receipt;
    case 'scheduled_ride_reminder':
      return NotificationCategory.scheduledRideReminder;
    default:
      return NotificationCategory.other;
  }
}

/// Client-side model of the backend Notification entity
/// (backend/internal/modules/notifications). Fields mirror the wire format of
/// GET /notifications/inbox and the WS `notification.user.{userId}` topic.
class AppNotification {
  final String id;
  final String userId;
  final NotificationChannel channel;
  final NotificationCategory category;
  final String title;
  final String body;

  /// Deep link consumed by NotificationRouter (a GoRoute path or a
  /// `nidus://`-style scheme registered there). May be empty when the
  /// notification has no navigable surface — the inbox then shows it without
  /// tap-to-navigate rather than inventing a destination.
  final String deepLink;

  /// Arbitrary JSON payload attached by the dispatcher (e.g. ride_id).
  final Map<String, dynamic> payload;

  final DateTime createdAt;
  final DateTime? readAt;
  final DateTime? deliveredAt;
  final DateTime? failedAt;
  final String? failureReason;

  const AppNotification({
    required this.id,
    required this.userId,
    required this.channel,
    required this.category,
    required this.title,
    required this.body,
    required this.deepLink,
    required this.payload,
    required this.createdAt,
    this.readAt,
    this.deliveredAt,
    this.failedAt,
    this.failureReason,
  });

  bool get isUnread => readAt == null;

  String? get rideId {
    final raw = payload['ride_id'] ?? payload['rideId'];
    return raw is String && raw.isNotEmpty ? raw : null;
  }

  static AppNotification fromJson(Map<String, dynamic> json) {
    DateTime? parseDate(Object? v) =>
        v is String && v.isNotEmpty ? DateTime.tryParse(v) : null;

    final payloadRaw = json['payload'];
    Map<String, dynamic> payloadMap;
    if (payloadRaw is Map<String, dynamic>) {
      payloadMap = payloadRaw;
    } else if (payloadRaw is String && payloadRaw.isNotEmpty) {
      try {
        final decoded = jsonDecode(payloadRaw);
        payloadMap = decoded is Map<String, dynamic> ? decoded : {};
      } on FormatException {
        payloadMap = {};
      }
    } else {
      payloadMap = {};
    }

    return AppNotification(
      id: '${json['id'] ?? ''}',
      userId: '${json['user_id'] ?? ''}',
      channel:
          notificationChannelFromWire('${json['channel'] ?? 'in_app'}'),
      category:
          notificationCategoryFromWire('${json['category'] ?? 'other'}'),
      title: '${json['title'] ?? ''}',
      body: '${json['body'] ?? ''}',
      deepLink: '${json['deep_link'] ?? ''}',
      payload: payloadMap,
      createdAt: parseDate(json['created_at']) ?? DateTime.now().toUtc(),
      readAt: parseDate(json['read_at']),
      deliveredAt: parseDate(json['delivered_at']),
      failedAt: parseDate(json['failed_at']),
      failureReason: json['failure_reason'] as String?,
    );
  }
}

/// One page of the paginated inbox returned by GET /notifications/inbox.
class InboxPage {
  final List<AppNotification> items;

  /// Opaque cursor for the next page; null means "no more pages".
  final String? nextCursor;

  const InboxPage({required this.items, this.nextCursor});

  static InboxPage fromJson(Map<String, dynamic> json) {
    final rawItems = json['items'] ?? json['notifications'] ?? const [];
    final items = <AppNotification>[];
    if (rawItems is List) {
      for (final e in rawItems.whereType<Map<String, dynamic>>()) {
        items.add(AppNotification.fromJson(e));
      }
    }
    final cursor = json['next_cursor'] ?? json['nextCursor'];
    return InboxPage(
      items: items,
      nextCursor: cursor is String && cursor.isNotEmpty ? cursor : null,
    );
  }
}
