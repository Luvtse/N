// §2.3 Pre-selfie verification — rider-side "Verify your driver" card.
//
// Honesty contract (CONTRIBUTING-RIDE-PARITY Global Rule 2): this widget has
// exactly one rendering per backend state and never invents imagery.
//   available    -> tappable card; opens the full-screen selfie-vs-profile
//                   comparison, fetching bytes through the server-minted
//                   signed redeem path only.
//   not_sent     -> "Driver did not send a selfie" + contact-driver prompt.
//   expired      -> "Selfie link expired" + re-check-ride action.
//   unavailable  -> "Verification unavailable" + retry action (our stack or
//                   the feature flag failed — NOT blamed on the driver).
//   unknown      -> treated as unavailable: an unrecognized wire value must
//                   never be rendered as success.
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:get_it/get_it.dart';

import '../../../../core/theme/app_colors.dart';
import '../../data/repositories/pre_selfie_repository.dart';

class VerifyDriverCard extends StatefulWidget {
  final PreSelfieInfo? preSelfie;

  /// Driver profile photo bytes (from the account service) shown side-by-side
  /// with the selfie for comparison. Null = no profile photo on file; the
  /// comparison view then shows the selfie alone with an explicit note rather
  /// than a placeholder avatar pretending to be the registered photo.
  final Uint8List? profilePhotoBytes;
  final String driverName;

  /// Re-poll the ride so the bloc refetches a fresh pre_selfie block
  /// (used by the expired/unavailable retry actions).
  final VoidCallback onRecheck;

  /// Open chat / calling surface ("contact driver" affordance for not_sent).
  final VoidCallback onContactDriver;

  const VerifyDriverCard({
    super.key,
    required this.preSelfie,
    required this.driverName,
    required this.onRecheck,
    required this.onContactDriver,
    this.profilePhotoBytes,
  });

  @override
  State<VerifyDriverCard> createState() => _VerifyDriverCardState();
}

class _VerifyDriverCardState extends State<VerifyDriverCard> {
  bool _busy = false;

  PreSelfieInfo get _info => widget.preSelfie ?? PreSelfieInfo.absent;

  Future<void> _openComparison() async {
    if (_busy || !_info.isAvailable) return;
    setState(() => _busy = true);
    try {
      final bytes = await GetIt.instance<PreSelfieRepository>()
          .fetchSelfieBytes(_info.redeemPath);
      if (!mounted) return;
      setState(() => _busy = false);
      await Navigator.of(context).push(
        MaterialPageRoute(
          builder: (_) => SelfieComparisonView(
            driverName: widget.driverName,
            selfieBytes: bytes,
            profilePhotoBytes: widget.profilePhotoBytes,
            expiresAtUtc: _info.expiresAtUtc,
          ),
        ),
      );
    } on PreSelfieLinkExpiredException {
      if (!mounted) return;
      setState(() => _busy = false);
      _showSheetMessage(
        'The selfie link expired before it could load. Pull a fresh ride '
        'state to get a new signed link.',
      );
      widget.onRecheck();
    } on PreSelfieStorageUnavailableException {
      if (!mounted) return;
      setState(() => _busy = false);
      _showSheetMessage(
        'Verification is temporarily unavailable on our side. The driver did '
        'send a selfie — try again in a moment.',
      );
    } on PreSelfieNetworkException catch (e) {
      if (!mounted) return;
      setState(() => _busy = false);
      _showSheetMessage(
        'Could not load the selfie (network error${e.statusCode != null ? ' HTTP ${e.statusCode}' : ''}). Tap to retry.',
      );
    } catch (_) {
      if (!mounted) return;
      setState(() => _busy = false);
      // Defensive: any unmapped failure still must not show a fake image.
      _showSheetMessage('Could not load the selfie. Tap to retry.');
    }
  }

  void _showSheetMessage(String message) {
    ScaffoldMessenger.of(context)
      ..hideCurrentSnackBar()
      ..showSnackBar(SnackBar(content: Text(message)));
  }

  @override
  Widget build(BuildContext context) {
    final info = _info;
    switch (info.state) {
      case PreSelfieState.available:
        return _card(
          icon: Icons.verified_user_outlined,
          iconColor: AppColors.success,
          title: 'Verify your driver',
          subtitle:
              '${widget.driverName} sent a selfie before picking you up. '
              'Tap to compare it with their profile photo.',
          trailing: _busy
              ? const SizedBox(
                  width: 20, height: 20,
                  child: CircularProgressIndicator(strokeWidth: 2))
              : const Icon(Icons.chevron_right),
          onTap: _openComparison,
        );
      case PreSelfieState.notSent:
        return _card(
          icon: Icons.person_off_outlined,
          iconColor: AppColors.warning,
          title: 'Driver did not send a selfie',
          subtitle: 'Spot your driver by plate and vehicle. You can still '
              'message or call them before entering.',
          trailing: TextButton(
            onPressed: widget.onContactDriver,
            child: const Text('Contact driver'),
          ),
          onTap: null,
        );
      case PreSelfieState.expired:
        return _card(
          icon: Icons.schedule,
          iconColor: AppColors.warning,
          title: 'Selfie link expired',
          subtitle: 'Signed links last 15 minutes. Ask your driver to resend, '
              'or refresh the ride.',
          trailing: TextButton(
            onPressed: widget.onRecheck,
            child: const Text('Refresh'),
          ),
          onTap: null,
        );
      case PreSelfieState.unavailable:
      case PreSelfieState.unknown:
        // unknown is deliberately folded into the honest-unavailable render:
        // an unrecognized wire value must never be presented as verified.
        return _card(
          icon: Icons.help_outline,
          iconColor: AppColors.textSecondary,
          title: 'Verification unavailable',
          subtitle: info.state == PreSelfieState.unknown
              ? 'The verification service returned an unexpected state. This '
                  'is our side, not the driver — retry below.'
              : 'Driver photo verification is not available for this ride '
                  'right now. Spot your driver by plate and vehicle.',
          trailing: TextButton(
            onPressed: widget.onRecheck,
            child: const Text('Retry'),
          ),
          onTap: null,
        );
    }
  }

  Widget _card({
    required IconData icon,
    required Color iconColor,
    required String title,
    required String subtitle,
    required Widget trailing,
    required VoidCallback? onTap,
  }) {
    return Card(
      margin: EdgeInsets.zero,
      child: ListTile(
        leading: Icon(icon, color: iconColor),
        title: Text(title, style: const TextStyle(fontWeight: FontWeight.w600)),
        subtitle: Padding(
          padding: const EdgeInsets.only(top: 4),
          child: Text(subtitle),
        ),
        trailing: trailing,
        onTap: onTap,
      ),
    );
  }
}

/// Full-screen side-by-side comparison of the just-sent selfie against the
/// driver's registered profile photo. Both images are decoded from real
/// fetched bytes; a missing profile photo is stated explicitly instead of
/// showing an avatar that could be mistaken for the registered image.
class SelfieComparisonView extends StatelessWidget {
  final String driverName;
  final Uint8List selfieBytes;
  final Uint8List? profilePhotoBytes;
  final DateTime? expiresAtUtc;

  const SelfieComparisonView({
    super.key,
    required this.driverName,
    required this.selfieBytes,
    required this.profilePhotoBytes,
    required this.expiresAtUtc,
  });

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: Text('Verify ${driverName.split(' ').first}')),
      body: SafeArea(
        child: Column(
          children: [
          Expanded(
              child: Padding(
                padding: const EdgeInsets.all(16),
                child: Row(
                  children: [
                    Expanded(
                      child: _ImagePanel(
                        label: 'Profile photo (on file)',
                        bytes: profilePhotoBytes,
                        missingNote:
                            'No profile photo is on file for this driver, so '
                            'there is nothing to compare against yet.',
                      ),
                    ),
                    const SizedBox(width: 16),
                    Expanded(
                      child: _ImagePanel(
                        label: 'Just-sent selfie',
                        bytes: selfieBytes,
                        missingNote: '',
                      ),
                    ),
                  ],
                ),
              ),
            ),
            if (expiresAtUtc != null)
              Padding(
                padding: const EdgeInsets.only(bottom: 8),
                child: Text(
                  'Selfie captured for this pickup only · valid until '
                  '${expiresAtUtc!.toLocal().toString().substring(0, 16)}',
                  style: Theme.of(context).textTheme.bodySmall,
                ),
              ),
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 0, 16, 16),
              child: Row(
                children: [
                  Expanded(
                    child: OutlinedButton.icon(
                      onPressed: () => Navigator.of(context).pop(false),
                      icon: const Icon(Icons.close),
                      label: const Text("That's not them"),
                    ),
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: ElevatedButton.icon(
                      style: ElevatedButton.styleFrom(
                        backgroundColor: AppColors.success,
                        foregroundColor: AppColors.textOnDark,
                      ),
                      onPressed: () => Navigator.of(context).pop(true),
                      icon: const Icon(Icons.check),
                      label: const Text('Looks right'),
                    ),
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _ImagePanel extends StatelessWidget {
  final String label;
  final Uint8List? bytes;
  final String missingNote;

  const _ImagePanel({
    required this.label,
    required this.bytes,
    required this.missingNote,
  });

  @override
  Widget build(BuildContext context) {
    final data = bytes;
    return Column(
      children: [
        Text(label, style: const TextStyle(fontWeight: FontWeight.w600)),
        const SizedBox(height: 8),
        Expanded(
          child: data == null
              ? Container(
                  width: double.infinity,
                  decoration: BoxDecoration(
                    color: AppColors.inputBackground,
                    borderRadius: BorderRadius.circular(12),
                  ),
                  alignment: Alignment.center,
                  padding: const EdgeInsets.all(16),
                  child: Text(
                    missingNote,
                    textAlign: TextAlign.center,
                    style: TextStyle(color: AppColors.textSecondary),
                  ),
                )
              : ClipRRect(
                  borderRadius: BorderRadius.circular(12),
                  child: Image.memory(
                    data,
                    fit: BoxFit.cover,
                    width: double.infinity,
                    errorBuilder: (_, __, ___) => Container(
                      color: AppColors.inputBackground,
                      alignment: Alignment.center,
                      child: const Text(
                        'Image failed to decode — the bytes were corrupt, '
                        'not absent. Retry from the ride screen.',
                        textAlign: TextAlign.center,
                      ),
                    ),
                  ),
                ),
        ),
      ],
    );
  }
}
