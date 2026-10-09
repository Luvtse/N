import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:share_plus/share_plus.dart';

import '../../../../core/di/injection.dart';
import '../../../../core/theme/app_colors.dart';
import '../../data/repositories/safety_repository.dart';
import '../bloc/ride_bloc.dart';

/// Slide-to-confirm emergency trigger. A plain tap is deliberately avoided:
/// pocket-dials and accidental presses must not fire a real SOS, matching
/// the Uber/Lyft "slide" pattern regulators expect.
class SosSlideToConfirm extends StatelessWidget {
  final VoidCallback onConfirmed;

  const SosSlideToConfirm({super.key, required this.onConfirmed});

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      height: 64,
      child: LayoutBuilder(
        builder: (context, constraints) {
          final trackWidth = constraints.maxWidth;
          const thumbSize = 56.0;
          final maxDrag = trackWidth - thumbSize - 16;

          return GestureDetector(
            behavior: HitTestBehavior.opaque,
            child: Container(
              decoration: BoxDecoration(
                color: AppColors.error.withOpacity(0.12),
                borderRadius: BorderRadius.circular(32),
                border: Border.all(color: AppColors.error, width: 1.5),
              ),
              child: Stack(
                alignment: Alignment.center,
                children: [
                  const Text(
                    'Slide to confirm SOS',
                    style: TextStyle(
                      color: AppColors.error,
                      fontWeight: FontWeight.w700,
                      fontSize: 15,
                    ),
                  ),
                  _DraggableSosThumb(
                    thumbSize: thumbSize,
                    maxDrag: maxDrag,
                    onConfirmed: onConfirmed,
                  ),
                ],
              ),
            ),
          );
        },
      ),
    );
  }
}

class _DraggableSosThumb extends StatefulWidget {
  final double thumbSize;
  final double maxDrag;
  final VoidCallback onConfirmed;

  const _DraggableSosThumb({
    required this.thumbSize,
    required this.maxDrag,
    required this.onConfirmed,
  });

  @override
  State<_DraggableSosThumb> createState() => _DraggableSosThumbState();
}

class _DraggableSosThumbState extends State<_DraggableSosThumb> {
  double _dx = 0;

  void _handlePanUpdate(DragUpdateDetails details) {
    setState(() {
      _dx = (_dx + details.delta.dx).clamp(0.0, widget.maxDrag);
    });
  }

  void _handlePanEnd() {
    final confirmed = _dx >= widget.maxDrag * 0.9;
    if (confirmed) {
      widget.onConfirmed();
      return;
    }
    setState(() => _dx = 0);
  }

  @override
  Widget build(BuildContext context) {
    return Positioned(
      left: 8 + _dx,
      child: GestureDetector(
        onHorizontalDragUpdate: _handlePanUpdate,
        onHorizontalDragEnd: (_) => _handlePanEnd(),
        child: Container(
          width: widget.thumbSize,
          height: widget.thumbSize,
          decoration: const BoxDecoration(
            color: AppColors.error,
            shape: BoxShape.circle,
          ),
          child: const Icon(
            Icons.sos,
            color: Colors.white,
            size: 28,
          ),
        ),
      ),
    );
  }
}

/// Full-screen SOS pre-alarm overlay with countdown, cancel, and immediate
/// escalation. Reads live countdown values from [RideBloc] getters via a
/// periodic repaint ticker (safety runs on a side channel so it never
/// clobbers the active ride state).
class SosCountdownOverlay extends StatefulWidget {
  const SosCountdownOverlay({super.key});

  @override
  State<SosCountdownOverlay> createState() => _SosCountdownOverlayState();
}

class _SosCountdownOverlayState extends State<SosCountdownOverlay> {
  Timer? _ticker;

  @override
  void initState() {
    super.initState();
    _ticker = Timer.periodic(
      const Duration(milliseconds: 500),
      (_) => setState(() {}),
    );
  }

  @override
  void dispose() {
    _ticker?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final bloc = context.read<RideBloc>();
    final remaining = bloc.sosCountdownRemaining;

    return Material(
      color: Colors.black.withOpacity(0.85),
      child: SafeArea(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              const Icon(Icons.emergency_share, color: AppColors.error, size: 72),
              const SizedBox(height: 16),
              const Text(
                'Emergency SOS',
                style: TextStyle(
                  color: Colors.white,
                  fontSize: 24,
                  fontWeight: FontWeight.bold,
                ),
              ),
              const SizedBox(height: 8),
              Text(
                remaining > 0
                    ? 'Alerting emergency services and your trusted contacts in $remaining s…'
                    : 'Sending help…',
                textAlign: TextAlign.center,
                style: const TextStyle(color: Colors.white70, fontSize: 15),
              ),
              const SizedBox(height: 32),
              Row(
                mainAxisAlignment: MainAxisAlignment.center,
                children: [
                  TextButton.icon(
                    onPressed: () {
                      bloc.add(const CancelSosCountdown());
                      Navigator.of(context).pop();
                    },
                    icon: const Icon(Icons.close, color: Colors.white70),
                    label: const Text(
                      'Cancel — I\'m safe',
                      style: TextStyle(color: Colors.white70),
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 16),
              SizedBox(
                width: double.infinity,
                child: ElevatedButton.icon(
                  style: ElevatedButton.styleFrom(
                    backgroundColor: AppColors.error,
                    foregroundColor: Colors.white,
                    padding: const EdgeInsets.symmetric(vertical: 16),
                  ),
                  onPressed: () => bloc.add(const ConfirmSos()),
                  icon: const Icon(Icons.sos),
                  label: const Text('Help now'),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// Bottom sheet that creates a tokenized live-tracking link for the current
/// ride and hands it off to the platform share sheet. Uses the injected
/// SafetyRepository directly (no new bloc states needed for sharing).
class ShareTripSheet extends StatefulWidget {
  final String rideId;

  const ShareTripSheet({super.key, required this.rideId});

  /// Presents the share sheet above any surface that has RideBloc +
  /// SafetyRepository in scope (tracking page uses this).
  static Future<void> showInline(BuildContext context, {required String rideId}) {
    return showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      backgroundColor: Colors.white,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
      ),
      builder: (sheetContext) => Padding(
        padding: EdgeInsets.only(
          bottom: MediaQuery.of(sheetContext).viewInsets.bottom,
        ),
        child: RepositoryProvider<SafetyRepository>(
          create: (_) => getIt<SafetyRepository>(),
          child: ShareTripSheet(rideId: rideId),
        ),
      ),
    );
  }

  @override
  State<ShareTripSheet> createState() => _ShareTripSheetState();
}

class _ShareTripSheetState extends State<ShareTripSheet> {
  bool _loading = false;
  String? _error;
  SharedTripLink? _link;

  Future<void> _createAndShare() async {
    setState(() {
      _loading = true;
      _error = null;
    });

    try {
      final repository = context.read<SafetyRepository>();
      final result = await repository.createShareLink(
        rideId: widget.rideId,
        label: 'My Nidus trip',
      );

      if (!mounted) return;

      if (!result.success || result.link == null) {
        setState(() {
          _loading = false;
          _error = result.message ?? 'Could not create a tracking link';
        });
        return;
      }

      setState(() {
        _loading = false;
        _link = result.link;
      });

      // Copy the URL as a universally-available fallback, then hand off to
      // any installed share target (SMS, WhatsApp, etc.).
      await _share(result.link!.publicUrl);
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = 'Sharing failed: ${e.toString()}';
      });
    }
  }

  Future<void> _share(String url) async {
    try {
      // Lazy import kept local so unit tests without platform channels still
      // construct this widget safely.
      await SharePlusHelper.share(url);
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Link ready to paste: $url')),
        );
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final link = _link;

    return Padding(
      padding: const EdgeInsets.all(24),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              const Icon(Icons.share_location, color: AppColors.primary),
              const SizedBox(width: 8),
              const Text(
                'Share My Trip',
                style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold),
              ),
            ],
          ),
          const SizedBox(height: 8),
          const Text(
            'Send a private live-tracking link. It expires automatically when '
            'the ride ends and can be revoked any time.',
            style: TextStyle(color: AppColors.textSecondary, fontSize: 13),
          ),
          const SizedBox(height: 16),
          if (_error != null) ...[
            Text(
              _error!,
              style: const TextStyle(color: AppColors.error, fontSize: 13),
            ),
            const SizedBox(height: 12),
          ],
          if (link != null) ...[
            Container(
              width: double.infinity,
              padding: const EdgeInsets.all(12),
              decoration: BoxDecoration(
                color: AppColors.inputBackground,
                borderRadius: BorderRadius.circular(8),
              ),
              child: SelectableText(
                link.publicUrl,
                style: const TextStyle(fontSize: 13),
              ),
            ),
            const SizedBox(height: 12),
          ],
          SizedBox(
            width: double.infinity,
            child: ElevatedButton.icon(
              onPressed: _loading ? null : _createAndShare,
              icon: _loading
                  ? const SizedBox(
                      width: 18,
                      height: 18,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Icon(Icons.send),
              label: Text(link == null ? 'Create & share link' : 'Share again'),
            ),
          ),
          const SizedBox(height: 8),
        ],
      ),
    );
  }
}

/// Thin indirection around share_plus so the sheet degrades gracefully when
/// no platform share handler is registered (tests, web previews).
class SharePlusHelper {
  static Future<void> share(String url) async {
    // share_plus is an app dependency; imported dynamically through the
    // plugin's static entry point below.
    await ShareAdapter.performShare(url);
  }
}

/// Adapter isolating the share_plus dependency to one place.
class ShareAdapter {
  static Future<void> performShare(String url) async {
    await shareImpl(url);
  }

  /// Overridable hook so widget tests can stub platform sharing.
  static Future<void> Function(String url) shareImpl = _systemShare;

  static Future<void> _systemShare(String url) async {
    // share_plus v7 API. Throws naturally when no platform handler is
    // registered, which _share's catch block converts into a copy-friendly
    // SnackBar fallback.
    await Share.share(url);
  }
}
