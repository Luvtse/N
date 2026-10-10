import 'dart:async';

import 'package:flutter/material.dart';
import 'package:permission_handler/permission_handler.dart';

import '../../../../core/location/device_location_service.dart';
import '../../../../core/theme/app_colors.dart';

/// Wraps any location-dependent child. While permission is unresolved or
/// denied, the child is hidden and a rationale card is shown instead — but the
/// screen never blanks: callers keep non-location inputs (e.g. destination
/// typing) outside this gate.
///
/// States handled:
/// - denied → rationale + "Allow" button that triggers the OS prompt
/// - permanently denied → "Open Settings" deep-link via openAppSettings()
/// - service disabled → "Turn on Location" (re-checks on resume)
/// - granted → renders [child]
class LocationPermissionGate extends StatefulWidget {
  final Widget child;
  final DeviceLocationService locationService;

  /// Re-evaluated whenever the app resumes (returning from OS settings).
  const LocationPermissionGate({
    super.key,
    required this.child,
    this.locationService = const DeviceLocationService(),
  });

  @override
  State<LocationPermissionGate> createState() => _LocationPermissionGateState();
}

class _LocationPermissionGateState extends State<LocationPermissionGate>
    with WidgetsBindingObserver {
  _GateStatus _status = _GateStatus.checking;
  StreamSubscription<AppLifecycleState>? _lifecycleSub;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _evaluate();
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _lifecycleSub?.cancel();
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    // Returning from OS settings: re-check so the gate opens without restart.
    if (state == AppLifecycleState.resumed &&
        _status != _GateStatus.granted) {
      _evaluate();
    }
  }

  Future<void> _evaluate() async {
    final serviceEnabled = await widget.locationService
        .getCurrentPosition(accuracy: LocationAccuracyLevel.low)
        .then((r) => r is! LocationServiceDisabled);
    if (!serviceEnabled) {
      if (mounted) setState(() => _status = _GateStatus.serviceDisabled);
      return;
    }
    final granted = await widget.locationService.hasPermission();
    if (!mounted) return;
    if (granted) {
      setState(() => _status = _GateStatus.granted);
      return;
    }
    final status = await Permission.location.status;
    if (!mounted) return;
    setState(() {
      _status = status.isPermanentlyDenied
          ? _GateStatus.permanentlyDenied
          : _GateStatus.denied;
    });
  }

  Future<void> _requestPermission() async {
    final result = await widget.locationService.getCurrentPosition();
    if (!mounted) return;
    switch (result) {
      case LocationGranted():
        setState(() => _status = _GateStatus.granted);
      case LocationPermissionPermanentlyDenied():
        setState(() => _status = _GateStatus.permanentlyDenied);
      case LocationServiceDisabled():
        setState(() => _status = _GateStatus.serviceDisabled);
      default:
        setState(() => _status = _GateStatus.denied);
    }
  }

  @override
  Widget build(BuildContext context) {
    return switch (_status) {
      _GateStatus.checking => const Padding(
          padding: EdgeInsets.all(24),
          child: Center(
            child: SizedBox(
              width: 28,
              height: 28,
              child: CircularProgressIndicator(strokeWidth: 2.5),
            ),
          ),
        ),
      _GateStatus.granted => widget.child,
      _GateStatus.denied => _RationaleCard(
          icon: Icons.location_off_outlined,
          title: 'Location needed for pickup',
          body: 'We use your location to drop the pickup pin where you stand. '
              'You can still type a destination without it.',
          actionLabel: 'Allow',
          onAction: _requestPermission,
        ),
      _GateStatus.permanentlyDenied => _RationaleCard(
          icon: Icons.settings_outlined,
          title: 'Location permission is off',
          body: 'Nidus was denied location access permanently. Open system '
              'settings and enable it for this app.',
          actionLabel: 'Open Settings',
          onAction: () async {
            await openAppSettings();
          },
        ),
      _GateStatus.serviceDisabled => _RationaleCard(
          icon: Icons.location_disabled_outlined,
          title: 'Location services are off',
          body: 'Turn on GPS / location services in your device settings so we '
              'can find nearby drivers.',
          actionLabel: 'Turn on Location',
          onAction: () async {
            await openAppSettings();
            _evaluate();
          },
        ),
    };
  }
}

enum _GateStatus { checking, granted, denied, permanentlyDenied, serviceDisabled }

class _RationaleCard extends StatelessWidget {
  final IconData icon;
  final String title;
  final String body;
  final String actionLabel;
  final Future<void> Function() onAction;

  const _RationaleCard({
    required this.icon,
    required this.title,
    required this.body,
    required this.actionLabel,
    required this.onAction,
  });

  @override
  Widget build(BuildContext context) {
    return Card(
      margin: const EdgeInsets.all(16),
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(16)),
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(icon, size: 40, color: AppColors.primary),
            const SizedBox(height: 12),
            Text(
              title,
              style: const TextStyle(
                fontSize: 16,
                fontWeight: FontWeight.w600,
                color: AppColors.textPrimary,
              ),
              textAlign: TextAlign.center,
            ),
            const SizedBox(height: 8),
            Text(
              body,
              style: const TextStyle(
                fontSize: 13,
                color: AppColors.textSecondary,
              ),
              textAlign: TextAlign.center,
            ),
            const SizedBox(height: 16),
            FilledButton(
              onPressed: () => onAction(),
              child: Text(actionLabel),
            ),
          ],
        ),
      ),
    );
  }
}
