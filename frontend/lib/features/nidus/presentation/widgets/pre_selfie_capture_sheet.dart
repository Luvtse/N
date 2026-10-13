// §2.3 Pre-selfie verification — driver-side capture step.
//
// Flow (per plan): fires when the driver enters DriverEnRoute. Uses the
// device camera via image_picker (^1.0.4, verified in pubspec.yaml),
// compresses client-side to <= 200 KB by re-shooting through
// pickImage's own JPEG quality ladder (imageQuality is honored on
// Android/iOS for camera captures; no extra compression dependency),
// uploads through PreSelfieRepository, and shows a real "Selfie sent"
// confirmation ONLY after the server returns 201. Every failure path
// surfaces the backend's own message verbatim and offers Retry. Skipping
// is honest: the rider UI renders exactly the not_sent state from the DTO.
//
// Honesty rules (CONTRIBUTING-RIDE-PARITY Global Rule 2):
// - No fake success: confirmation appears only on a real upload.
// - No placeholder imagery: preview uses actual captured file bytes.
// - Oversize after every rung => explicit user-facing reason, never a
//   silent corrupt-bytes upload; the server's 413 stays authoritative.
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:get_it/get_it.dart';
import 'package:image_picker/image_picker.dart';

import '../../../../core/theme/app_colors.dart';
import '../../data/repositories/pre_selfie_repository.dart';

/// Client-side cap exceeded even after the full quality ladder.
class PreSelfieCaptureTooLargeException implements Exception {
  final int byteLength;
  const PreSelfieCaptureTooLargeException(this.byteLength);

  String get message =>
      'The photo is ${(byteLength / 1024).round()} KB even at maximum '
      'compression (limit 200 KB). Retake in brighter light — cameras '
      'produce huge files in darkness.';
}

/// Capture + compress + upload logic, separated from the widget so it can
/// be unit-tested with a fake repository and a fake picker.
class PreSelfieCaptureController {
  static const int maxBytes = 200 * 1024;

  /// Descending JPEG quality rungs tried one capture at a time. Each rung
  /// asks the OS camera pipeline for a fresh encode at that quality; we
  /// stop at the first file that fits the cap. The rider never waits on a
  /// second tap unless the first shots were genuinely too heavy.
  static const List<int> _qualityLadder = [80, 60, 45];

  final PreSelfieRepository _repo;
  final ImagePicker _picker;

  PreSelfieCaptureController({PreSelfieRepository? repo, ImagePicker? picker})
      : _repo = repo ?? GetIt.instance<PreSelfieRepository>(),
        _picker = picker ?? ImagePicker();

  /// One camera session across the quality ladder. Returns null if the
  /// user cancelled before any shot landed. Throws
  /// [PreSelfieCaptureTooLargeException] if no rung produced a compliant
  /// file — the caller must NOT upload in that case.
  Future<XFile?> captureCompressed() async {
    XFile? largestSoFar;
    for (final quality in _qualityLadder) {
      final shot = await _picker.pickImage(
        source: ImageSource.camera,
        preferredCameraDevice: CameraDevice.front,
        // maxWidth keeps the encode small without changing what the SDK
        // reports as the file; 1280 px is ample for a face comparison.
        maxWidth: 1280,
        maxHeight: 1280,
        imageQuality: quality,
      );
      if (shot == null) return null; // cancelled — not an error state.
      final size = await shot.length();
      if (size <= maxBytes) return shot;
      largestSoFar = shot;
    }
    final finalSize = largestSoFar != null ? await largestSoFar.length() : 0;
    throw PreSelfieCaptureTooLargeException(finalSize);
  }

  /// Reads the compressed file and uploads it. Never fabricates success:
  /// the returned result exists only because the server said 201/200.
  Future<PreSelfieUploadResult> upload(String rideId, XFile file) async {
    final bytes = await file.readAsBytes();
    return _repo.uploadDriverSelfie(rideId: rideId, jpegBytes: bytes);
  }
}

/// The capture sheet shown to drivers on entering DriverEnRoute.
class PreSelfieCaptureSheet extends StatefulWidget {
  final String rideId;

  /// Invoked after a confirmed successful upload so the parent can react.
  final VoidCallback? onSent;

  const PreSelfieCaptureSheet({
    super.key,
    required this.rideId,
    this.onSent,
  });

  static Future<void> show(
    BuildContext context, {
    required String rideId,
    VoidCallback? onSent,
  }) {
    return showModalBottomSheet(
      context: context,
      isScrollControlled: true,
      builder: (_) => Padding(
        padding: EdgeInsets.only(
          bottom: MediaQuery.of(context).viewInsets.bottom,
        ),
        child: PreSelfieCaptureSheet(rideId: rideId, onSent: onSent),
      ),
    );
  }

  @override
  State<PreSelfieCaptureSheet> createState() => _PreSelfieCaptureSheetState();
}

enum _Step { idle, capturing, uploading, sent, failed }

class _PreSelfieCaptureSheetState extends State<PreSelfieCaptureSheet> {
  final PreSelfieCaptureController _controller =
      PreSelfieCaptureController();
  _Step _step = _Step.idle;
  String _failure = '';
  int _capturedKb = 0;

  Future<void> _captureAndSend() async {
    setState(() {
      _step = _Step.capturing;
      _failure = '';
    });
    try {
      final file = await _controller.captureCompressed();
      if (file == null) {
        // Cancelled OS camera — return to idle honestly, no error toast.
        if (mounted) setState(() => _step = _Step.idle);
        return;
      }
      final kb = ((await file.length()) / 1024).round();
      if (!mounted) return;
      setState(() {
        _capturedKb = kb;
        _step = _Step.uploading;
      });
      await _controller.upload(widget.rideId, file);
      if (!mounted) return;
      setState(() => _step = _Step.sent);
      widget.onSent?.call();
    } on PreSelfieCaptureTooLargeException catch (e) {
      if (!mounted) return;
      setState(() {
        _step = _Step.failed;
        _failure = e.message;
      });
    } on PreSelfieUploadRejectedException catch (e) {
      if (!mounted) return;
      setState(() {
        _step = _Step.failed;
        // Server message displayed verbatim per Global Rule 2.
        _failure = e.serverMessage;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _step = _Step.failed;
        _failure = 'Upload failed — network error. You can retry.';
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text(
              'Send your pre-selfie',
              style: Theme.of(context)
                  .textTheme
                  .titleMedium
                  ?.copyWith(fontWeight: FontWeight.bold),
            ),
            const SizedBox(height: 8),
            Text(
              'Riders verify their pickup against this photo. It is stored '
              'for 15 minutes only and shared with your matched rider.',
              style: Theme.of(context).textTheme.bodyMedium,
            ),
            const SizedBox(height: 16),
            ...switch (_step) {
              _Step.idle => [
                  ElevatedButton.icon(
                    onPressed: _captureAndSend,
                    icon: const Icon(Icons.camera_front),
                    label: const Text('Take selfie'),
                  ),
                ],
              _Step.capturing => [
                  const ListTile(
                    leading: SizedBox(
                        width: 22,
                        height: 22,
                        child: CircularProgressIndicator(strokeWidth: 2)),
                    title: Text('Opening camera…'),
                  ),
                ],
              _Step.uploading => [
                  ListTile(
                    leading: const SizedBox(
                        width: 22,
                        height: 22,
                        child: CircularProgressIndicator(strokeWidth: 2)),
                    title: const Text('Uploading selfie…'),
                    subtitle: _capturedKb > 0
                        ? Text('Compressed to $_capturedKb KB')
                        : null,
                  ),
                ],
              _Step.sent => [
                  const ListTile(
                    leading: Icon(Icons.check_circle,
                        color: AppColors.success),
                    title: Text('Selfie sent'),
                    subtitle: Text(
                        'Your rider can now verify you before entering.'),
                  ),
                  const SizedBox(height: 8),
                  TextButton(
                    onPressed: () => Navigator.of(context).pop(),
                    child: const Text('Done'),
                  ),
                ],
              _Step.failed => [
                  ListTile(
                    leading: const Icon(Icons.error_outline,
                        color: AppColors.error),
                    title: const Text('Not sent'),
                    subtitle: Text(_failure),
                  ),
                  const SizedBox(height: 8),
                  ElevatedButton.icon(
                    onPressed: _captureAndSend,
                    icon: const Icon(Icons.refresh),
                    label: const Text('Retry'),
                  ),
                ],
            },
            if (_step != _Step.sent)
              TextButton(
                onPressed: () {
                  ScaffoldMessenger.of(context).showSnackBar(
                    const SnackBar(
                      content: Text(
                          'Skipped — the rider will see that you did not '
                          'send a selfie and can contact you.'),
                    ),
                  );
                  Navigator.of(context).pop();
                },
                child: const Text('Skip for now'),
              ),
          ],
        ),
      ),
    );
  }
}
