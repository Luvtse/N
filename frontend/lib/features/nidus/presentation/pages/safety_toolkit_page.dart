import 'package:flutter/material.dart';

import '../../../../core/theme/app_colors.dart';
import '../bloc/ride_bloc.dart';
import '../widgets/safety_widgets.dart';

/// Safety Toolkit hub — the shield-icon entry point regulators expect on
/// every active trip. All actions operate on the live [RideBloc] so the
/// toolkit works from any tracking surface without prop drilling.
class SafetyToolkitPage extends StatelessWidget {
  const SafetyToolkitPage({super.key});

  /// Presents the toolkit as a full-screen route pushed above tracking.
  static Future<void> open(BuildContext context) {
    return Navigator.of(context).push<void>(
      MaterialPageRoute<void>(
        builder: (_) => const SafetyToolkitPage(),
      ),
    );
  }

  static void _requireActiveRide(BuildContext context) {
    final bloc = context.read<RideBloc>();
    final state = bloc.state;
    if (state is DriverMatched ||
        state is DriverEnRoute ||
        state is RideInProgress ||
        state is RideRequested) {
      return;
    }
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(
        content: Text('Start a ride to use safety features.'),
        backgroundColor: AppColors.warning,
      ),
    );
  }

  static String? _activeRideId(BuildContext context) {
    final state = context.read<RideBloc>().state;
    if (state is RideRequested) return state.rideId;
    if (state is DriverMatched) return state.rideId;
    if (state is DriverEnRoute) return state.rideId;
    return null;
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Safety'),
        leading: IconButton(
          icon: const Icon(Icons.arrow_back),
          onPressed: () => Navigator.of(context).pop(),
        ),
      ),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          _SafetyActionTile(
            icon: Icons.sos,
            iconColor: AppColors.error,
            title: 'Emergency SOS',
            subtitle:
                'Alert emergency contacts and share your live location. '
                'You get a 5-second window to cancel before it fires.',
            onTap: () {
              final rideId = _activeRideId(context);
              if (rideId == null) {
                _requireActiveRide(context);
                return;
              }
              showModalBottomSheet<void>(
                context: context,
                isScrollControlled: true,
                backgroundColor: Colors.transparent,
                builder: (_) => const SosCountdownOverlay(),
              );
            },
          ),
          _SafetyActionTile(
            icon: Icons.share_location,
            iconColor: AppColors.primary,
            title: 'Share My Trip',
            subtitle:
                'Send a private live-tracking link. Expires automatically '
                'when the ride ends.',
            onTap: () {
              final rideId = _activeRideId(context);
              if (rideId == null) {
                _requireActiveRide(context);
                return;
              }
              showModalBottomSheet<void>(
                context: context,
                isScrollControlled: true,
                backgroundColor: Colors.white,
                shape: const RoundedRectangleBorder(
                  borderRadius:
                      BorderRadius.vertical(top: Radius.circular(24)),
                ),
                builder: (sheetContext) => Padding(
                  padding: EdgeInsets.only(
                    bottom: MediaQuery.of(sheetContext).viewInsets.bottom,
                  ),
                  child: ShareTripSheet(rideId: rideId),
                ),
              );
            },
          ),
          _SafetyActionTile(
            icon: Icons.shield_outlined,
            iconColor: AppColors.success,
            title: 'Verify your driver',
            subtitle:
                'Before getting in: match the photo, plate, and model on '
                'the driver card. Share the pickup PIN only with the '
                'matching driver.',
            onTap: () {
              ScaffoldMessenger.of(context).showSnackBar(
                const SnackBar(
                  content: Text(
                      'If anything does not match, cancel free of charge '
                          'and report it from this menu.'),
                ),
              );
            },
          ),
          _SafetyActionTile(
            icon: Icons.report_gmailerrorred_outlined,
            iconColor: AppColors.warning,
            title: 'Report driver conduct',
            subtitle:
                'Behavioral or safety concerns are routed to the trust & '
                'safety team, separate from billing disputes.',
            onTap: () {
              ScaffoldMessenger.of(context).showSnackBar(
                const SnackBar(
                  content: Text(
                      'Reports can also be filed from the receipt after '
                          'your trip ends.'),
                ),
              );
            },
          ),
          const SizedBox(height: 8),
          Container(
            padding: const EdgeInsets.all(16),
            decoration: BoxDecoration(
              color: AppColors.inputBackground,
              borderRadius: BorderRadius.circular(12),
            ),
            child: const Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  'Ethiopia emergency numbers',
                  style: TextStyle(fontWeight: FontWeight.w700, fontSize: 14),
                ),
                SizedBox(height: 8),
                Text('Police: 991 / 919\nAmbulance: 907\nFire: 939',
                    style: TextStyle(fontSize: 14)),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _SafetyActionTile extends StatelessWidget {
  final IconData icon;
  final Color iconColor;
  final String title;
  final String subtitle;
  final VoidCallback onTap;

  const _SafetyActionTile({
    required this.icon,
    required this.iconColor,
    required this.title,
    required this.subtitle,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    return Card(
      margin: const EdgeInsets.only(bottom: 12),
      child: ListTile(
        contentPadding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
        leading: CircleAvatar(
          backgroundColor: iconColor.withOpacity(0.12),
          child: Icon(icon, color: iconColor),
        ),
        title: Text(title,
            style: const TextStyle(fontWeight: FontWeight.w700)),
        subtitle: Text(subtitle, style: const TextStyle(fontSize: 12)),
        trailing: const Icon(Icons.chevron_right),
        onTap: onTap,
      ),
    );
  }
}
