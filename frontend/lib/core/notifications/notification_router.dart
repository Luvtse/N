import 'package:flutter/foundation.dart';

import 'notification_models.dart';

/// Maps notification deep links / categories to GoRouter paths (§1.4 step 5).
///
/// The backend attaches `deep_link` to every dispatched notification; this is
/// the single place that translates it into an in-app route. Unknown links
/// resolve to null so the caller can show the notification content inline
/// instead of navigating to a guessed destination.
class NotificationRouter {
  /// Resolves any notification (from FCM tap, in-app banner tap, or inbox row)
  /// to a GoRouter location string, or null when there is no trustworthy
  /// destination.
  ///
  /// Priority: explicit `deep_link` (when it is one we recognize) → category
  /// convention fallbacks documented below.
  static String? resolveLocation(AppNotification n) {
    final link = n.deepLink.trim();

    // Explicit links are honored verbatim when they target surfaces that
    // exist in app_router.dart. Anything else falls through to conventions.
    if (link.isNotEmpty && _isRoutable(link)) return link;

    switch (n.category) {
      case NotificationCategory.rideMatched:
      case NotificationCategory.driverArriving:
      case NotificationCategory.safety:
      case NotificationCategory.receipt:
        // All ride notifications route to the tracking page for that ride.
        final rideId = n.rideId;
        if (rideId != null) return '/nidus/tracking/$rideId';
        // No ride id means nothing safe to navigate to — render inline.
        return null;
      case NotificationCategory.paymentFailed:
        // Payment recovery lives on the wallet surface (preserved feature);
        // Phase 2 §3.1 adds a dedicated recovery sheet and this will prefer
        // the backend-provided deep_link once it exists.
        return '/wallet';
      case NotificationCategory.promo:
        // Promo surface lands in Phase 2 §3.2; until then there is no route
        // to invent — show the promo content in the inbox/banner instead.
        return null;
      case NotificationCategory.scheduledRideReminder:
        final rideId = n.rideId;
        return rideId != null ? '/nidus/tracking/$rideId' : '/nidus/history';
      case NotificationCategory.other:
        return null;
    }
  }

  /// Whitelist of path prefixes that actually exist in the single router.
  /// A deep_link outside this list is treated as unresolvable rather than fed
  /// to GoRouter and crashing on a missing route.
  static bool _isRoutable(String link) {
    if (!link.startsWith('/')) return false;
    const allowedPrefixes = [
      '/nidus/',
      '/haven/',
      '/vorax/',
      '/profile',
      '/settings',
      '/wallet',
      '/home',
    ];
    return allowedPrefixes.any(link.startsWith);
  }

  /// Icon + accent metadata used by both the in-app banner and the inbox rows,
  /// keyed by category — defined here once so the two surfaces never drift.
  static ({String label}) categoryLabel(NotificationCategory c) {
    switch (c) {
      case NotificationCategory.rideMatched:
        return (label: 'Driver matched');
      case NotificationCategory.driverArriving:
        return (label: 'Driver arriving');
      case NotificationCategory.paymentFailed:
        return (label: 'Payment failed');
      case NotificationCategory.promo:
        return (label: 'Promotion');
      case NotificationCategory.safety:
        return (label: 'Safety alert');
      case NotificationCategory.receipt:
        return (label: 'Receipt ready');
      case NotificationCategory.scheduledRideReminder:
        return (label: 'Scheduled ride');
      case NotificationCategory.other:
        return (label: 'Notification');
    }
  }

  /// Convenience for callers that only have raw FCM data maps.
  static String? resolveFromRawData(Map<String, dynamic> data) {
    final link = data['deep_link'];
    if (link is String && link.isNotEmpty && _isRoutable(link)) return link;
    debugPrint('[notif-router] no routable deep_link in payload keys: '
        '${data.keys.join(',')}');
    return null;
  }
}
