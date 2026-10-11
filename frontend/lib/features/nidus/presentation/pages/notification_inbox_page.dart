import 'package:flutter/material.dart';

import '../../../../core/di/injection.dart';
import '../../../../core/notifications/notification_models.dart';
import '../../../../core/notifications/notification_repository.dart';
import '../../../../core/notifications/notification_router.dart';
import '../../../../core/notifications/notification_service.dart';
import '../../../../core/theme/app_colors.dart';

/// Notification inbox surface (§1.4 step 7). Reached from the app-bar bell.
///
/// Lists GET /notifications/inbox with cursor pagination, marks rows read on
/// tap, and deep-links through NotificationRouter. Failure states are honest:
/// an error banner with Retry (re-fetches the first page) — never empty-list
/// masquerading as "no notifications" when the fetch actually failed.
class NotificationInboxPage extends StatefulWidget {
  const NotificationInboxPage({super.key});

  @override
  State<NotificationInboxPage> createState() => _NotificationInboxPageState();
}

enum _LoadState { loading, ready, failed }

class _NotificationInboxPageState extends State<NotificationInboxPage> {
  final NotificationService _service = getIt<NotificationService>();
  final NotificationRepository _repository = getIt<NotificationRepository>();

  final ScrollController _scrollController = ScrollController();
  final List<AppNotification> _items = [];

  _LoadState _state = _LoadState.loading;
  String? _loadError;
  String? _nextCursor;
  bool _loadingMore = false;

  @override
  void initState() {
    super.initState();
    _scrollController.addListener(_onScroll);
    _fetchFirstPage();
  }

  @override
  void dispose() {
    _scrollController.dispose();
    super.dispose();
  }

  Future<void> _fetchFirstPage() async {
    setState(() {
      _state = _LoadState.loading;
      _loadError = null;
    });
    try {
      final page = await _repository.fetchInbox();
      if (!mounted) return;
      setState(() {
        _items
          ..clear()
          ..addAll(page.items);
        _nextCursor = page.nextCursor;
        _state = _LoadState.ready;
      });
    } on NotificationApiException catch (e) {
      if (!mounted) return;
      setState(() {
        _state = _LoadState.failed;
        _loadError = e.message;
      });
    }
  }

  void _onScroll() {
    if (_scrollController.position.pixels >=
            _scrollController.position.maxScrollExtent - 200 &&
        !_loadingMore &&
        _nextCursor != null &&
        _state == _LoadState.ready) {
      _fetchMore();
    }
  }

  Future<void> _fetchMore() async {
    final cursor = _nextCursor;
    if (cursor == null) return;
    setState(() => _loadingMore = true);
    try {
      final page = await _repository.fetchInbox(cursor: cursor);
      if (!mounted) return;
      setState(() {
        _items.addAll(page.items);
        _nextCursor = page.nextCursor;
        _loadingMore = false;
      });
    } on NotificationApiException {
      if (!mounted) return;
      // Keep existing rows; surface a snackbar so pagination failure is
      // visible but not catastrophic.
      setState(() => _loadingMore = false);
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Could not load more notifications.')),
      );
    }
  }

  Future<void> _onTapRow(AppNotification n) async {
    final location = NotificationRouter.resolveLocation(n);
    if (n.isUnread) {
      await _service.markRead(n.id);
      if (!mounted) return;
      setState(() {
        final idx = _items.indexWhere((e) => e.id == n.id);
        if (idx != -1) {
          _items[idx] = AppNotification(
            id: n.id,
            userId: n.userId,
            channel: n.channel,
            category: n.category,
            title: n.title,
            body: n.body,
            deepLink: n.deepLink,
            payload: n.payload,
            createdAt: n.createdAt,
            readAt: DateTime.now().toUtc(),
            deliveredAt: n.deliveredAt,
            failedAt: n.failedAt,
            failureReason: n.failureReason,
          );
        }
      });
    }
    if (location != null && mounted) {
      Navigator.of(context).pop();
      context.go(location);
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
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Notifications'),
        backgroundColor: Theme.of(context).scaffoldBackgroundColor,
        foregroundColor: AppColors.textPrimary,
        elevation: 0,
      ),
      body: RefreshIndicator(
        onRefresh: _fetchFirstPage,
        child: _buildBody(),
      ),
    );
  }

  Widget _buildBody() {
    switch (_state) {
      case _LoadState.loading:
        return const Center(
          child: CircularProgressIndicator(color: AppColors.primary),
        );
      case _LoadState.failed:
        return ListView(
          children: [
            const SizedBox(height: 96),
            const Icon(Icons.cloud_off_rounded,
                size: 48, color: AppColors.textTertiary),
            const SizedBox(height: 12),
            Center(
              child: Text(
                'Inbox unavailable',
                style: TextStyle(
                    fontWeight: FontWeight.w700,
                    color: AppColors.textSecondary),
              ),
            ),
            if (_loadError != null) ...[
              const SizedBox(height: 4),
              Center(
                child: Text(
                  _loadError!,
                  style: const TextStyle(
                      fontSize: 12, color: AppColors.textTertiary),
                ),
              ),
            ],
            const SizedBox(height: 16),
            Center(
              child: FilledButton.icon(
                onPressed: _fetchFirstPage,
                icon: const Icon(Icons.refresh),
                label: const Text('Retry'),
              ),
            ),
          ],
        );
      case _LoadState.ready:
        if (_items.isEmpty) {
          return ListView(
            children: const [
              SizedBox(height: 120),
              Center(
                child: Text(
                  'No notifications yet.',
                  style: TextStyle(color: AppColors.textTertiary),
                ),
              ),
            ],
          );
        }
        return ListView.separated(
          itemCount: _items.length + (_loadingMore ? 1 : 0),
          separatorBuilder: (_, __) => const Divider(height: 1),
          itemBuilder: (context, index) {
            if (index >= _items.length) {
              return const Padding(
                padding: EdgeInsets.all(16),
                child: Center(
                  child: SizedBox(
                    width: 22,
                    height: 22,
                    child: CircularProgressIndicator(strokeWidth: 2.5),
                  ),
                ),
              );
            }
            final n = _items[index];
            return ListTile(
              onTap: () => _onTapRow(n),
              contentPadding:
                  const EdgeInsets.symmetric(horizontal: 20, vertical: 4),
              leading: CircleAvatar(
                backgroundColor:
                    n.isUnread ? AppColors.primary.withOpacity(0.12) : null,
                child: Icon(
                  _iconFor(n.category),
                  size: 20,
                  color: n.isUnread ? AppColors.primary : AppColors.textTertiary,
                ),
              ),
              title: Text(
                n.title.isEmpty
                    ? NotificationRouter.categoryLabel(n.category).label
                    : n.title,
                style: TextStyle(
                  fontWeight:
                      n.isUnread ? FontWeight.w700 : FontWeight.w500,
                ),
              ),
              subtitle: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  if (n.body.isNotEmpty)
                    Text(n.body,
                        maxLines: 2, overflow: TextOverflow.ellipsis),
                  Text(
                    _relativeTime(n.createdAt),
                    style: const TextStyle(
                        fontSize: 11, color: AppColors.textTertiary),
                  ),
                ],
              ),
              trailing: n.isUnread
                  ? Container(
                      width: 10,
                      height: 10,
                      decoration: const BoxDecoration(
                        color: AppColors.primary,
                        shape: BoxShape.circle,
                      ),
                    )
                  : null,
            );
          },
        );
    }
  }

  String _relativeTime(DateTime dt) {
    final diff = DateTime.now().toUtc().difference(dt);
    if (diff.inMinutes < 1) return 'just now';
    if (diff.inHours < 1) return '${diff.inMinutes} min ago';
    if (diff.inDays < 1) return '${diff.inHours} h ago';
    if (diff.inDays < 7) return '${diff.inDays} d ago';
    return '${dt.year}-${dt.month.toString().padLeft(2, '0')}-'
        '${dt.day.toString().padLeft(2, '0')}';
  }
}
