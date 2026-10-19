import 'dart:async';

import 'package:flutter/material.dart';

import '../../../../core/notifications/notification_models.dart';
import '../../../../core/notifications/notification_router.dart';
import '../../../../core/notifications/notification_service.dart';
import '../../../../core/theme/app_colors.dart';

/// Slide-down in-app notification banner (§1.4 step 6).
///
/// Mount once inside the app-shell's Stack so it overlays every route. It
/// subscribes to [NotificationService.bannerStream] (which already merges the
/// WS topic and FirebaseMessaging.onMessage) and shows one banner at a time:
/// category icon + title + body, auto-dismiss after 5 s, tap navigates via
/// [NotificationRouter]. When a notification has no trustworthy destination,
/// tapping simply dismisses it — we never guess a route.
class InAppNotificationBanner extends StatefulWidget {
  final NotificationService service;

  /// Optional hook used by hosts that own their GoRouter instance (the shell
  /// passes `context.go`). Defaults to Navigator-less no-op dismissal.
  final void Function(BuildContext context, String location)? onNavigate;

  const InAppNotificationBanner({
    super.key,
    required this.service,
    this.onNavigate,
  });

  @override
  State<InAppNotificationBanner> createState() =>
      _InAppNotificationBannerState();
}

class _InAppNotificationBannerState extends State<InAppNotificationBanner>
    with SingleTickerProviderStateMixin {
  static const Duration _autoDismiss = Duration(seconds: 5);

  StreamSubscription<AppNotification>? _sub;
  AppNotification? _current;
  late final AnimationController _controller;
  Timer? _dismissTimer;

  @override
  void initState() {
    super.initState();
    _controller = AnimationController(
      vsync: this,
      duration: const Duration(milliseconds: 300),
    );
    _sub = widget.service.bannerStream.listen(_show);
  }

  void _show(AppNotification n) {
    // Newest wins; if a banner is up, swap its content and restart timers.
    setState(() => _current = n);
    _controller.forward(from: 0);
    _dismissTimer?.cancel();
    _dismissTimer = Timer(_autoDismiss, _dismiss);
  }

  void _dismiss() {
    _dismissTimer?.cancel();
    if (!mounted || _current == null) return;
    _controller.reverse().then((_) {
      if (mounted) setState(() => _current = null);
    });
  }

  void _onTap() {
    final n = _current;
    if (n == null) return;
    final location = NotificationRouter.resolveLocation(n);
    _dismiss();
    if (location != null && widget.onNavigate != null && mounted) {
      widget.onNavigate!(context, location);
    }
  }

  IconData _iconFor(NotificationCategory c) {
    switch (c) {
      case NotificationCategory.rideMatched:
        return Icons.directions_car_filled_rounded;
      case NotificationCategory.driverArriving:
        return Icons.location_searching_rounded;
      case NotificationCategory.paymentFailed:
        return Icons.credit_card_gear_ready_rounded;
      case NotificationCategory.promo:
        return Icons.local_offer_rounded;
      case NotificationCategory.safety:
        return Icons.shield_rounded;
      case NotificationCategory.receipt:
        return Icons.receipt_long_rounded;
      case NotificationCategory.scheduledRideReminder:
        return Icons.schedule_rounded;
      case NotificationCategory.other:
        return Icons.notifications_rounded;
    }
  }

  @override
  void dispose() {
    _sub?.cancel();
    _dismissTimer?.cancel();
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final n = _current;
    if (n == null) return const SizedBox.shrink();

    final topInset = MediaQuery.of(context).padding.top;
    return Positioned(
      top: topInset + 8,
      left: 12,
      right: 12,
      child: AnimatedBuilder(
        animation: _controller,
        builder: (context, child) {
          final t = Curves.easeOutCubic.transform(_controller.value);
          return Transform.translate(
            offset: Offset(0, -60 * (1 - t)),
            child: Opacity(opacity: t, child: child),
          );
        },
        child: Material(
          color: Colors.transparent,
          child: InkWell(
            onTap: _onTap,
            borderRadius: BorderRadius.circular(14),
            child: Container(
              padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
              decoration: BoxDecoration(
                color: Theme.of(context).brightness == Brightness.dark
                    ? AppColors.primaryDark
                    : AppColors.surface,
                borderRadius: BorderRadius.circular(14),
                boxShadow: [
                  BoxShadow(
                    color: Colors.black.withOpacity(0.18),
                    blurRadius: 16,
                    offset: const Offset(0, 6),
                  ),
                ],
              ),
              child: Row(
                children: [
                  Icon(
                    _iconFor(n.category),
                    size: 22,
                    color: AppColors.primary,
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Text(
                          n.title.isEmpty
                              ? NotificationRouter.categoryLabel(n.category).label
                              : n.title,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: const TextStyle(
                            fontWeight: FontWeight.w700,
                            fontSize: 14,
                          ),
                        ),
                        if (n.body.isNotEmpty) ...[
                          const SizedBox(height: 2),
                          Text(
                            n.body,
                            maxLines: 2,
                            overflow: TextOverflow.ellipsis,
                            style: TextStyle(
                              fontSize: 13,
                              color: Theme.of(context)
                                  .textTheme
                                  .bodySmall
                                  ?.color,
                            ),
                          ),
                        ],
                      ],
                    ),
                  ),
                  const SizedBox(width: 8),
                  IconButton(
                    visualDensity: VisualDensity.compact,
                    icon: const Icon(Icons.close, size: 18),
                    tooltip: 'Dismiss',
                    onPressed: _dismiss,
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}
