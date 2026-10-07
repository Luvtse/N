import 'dart:io';

import 'package:flutter/material.dart';

import '../../../../core/theme/app_colors.dart';

/// Reason codes accepted by POST /api/v1/ledger/disputes (backend
/// ledger/application/commands/file_dispute.go). Keep this list in sync with
/// the backend's allowed reason enum.
class DisputeReason {
  final String code;
  final String label;

  /// Simple cases auto-resolve instantly on the backend (Phase F rules
  /// engine); complex ones enter the admin review queue.
  final bool autoResolvable;

  const DisputeReason({
    required this.code,
    required this.label,
    this.autoResolvable = false,
  });

  static const reasons = <DisputeReason>[
    DisputeReason(
      code: 'no_show',
      label: 'Driver did not show up',
      autoResolvable: true,
    ),
    DisputeReason(
      code: 'overcharge',
      label: 'Charged more than the fare shown',
    ),
    DisputeReason(
      code: 'route_deviation',
      label: 'Unreasonable route / extra distance',
    ),
    DisputeReason(
      code: 'vehicle_issue',
      label: 'Vehicle unsafe or not as described',
    ),
    DisputeReason(
      code: 'lost_item',
      label: 'Item left behind / lost property',
    ),
    DisputeReason(
      code: 'conduct',
      label: 'Driver conduct / safety concern',
    ),
    DisputeReason(code: 'other', label: 'Other'),
  ];

  static DisputeReason? byCode(String code) {
    for (final r in reasons) {
      if (r.code == code) return r;
    }
    return null;
  }
}

/// Result of a completed dispute form submission.
class DisputeSubmission {
  final String reasonCode;
  final String description;
  final List<String> evidenceUrls;

  const DisputeSubmission({
    required this.reasonCode,
    required this.description,
    this.evidenceUrls = const [],
  });
}

/// Bottom-sheet form for filing a ride dispute (Phase H Step 1:
/// "Dispute UI: Button on completed ride; Form for reason/evidence upload").
class DisputeFormSheet extends StatefulWidget {
  final String rideId;

  const DisputeFormSheet({super.key, required this.rideId});

  /// Returns a [DisputeSubmission] when the user submits, or null if the
  /// sheet was dismissed.
  static Future<DisputeSubmission?> show(
    BuildContext context, {
    required String rideId,
  }) {
    return showModalBottomSheet<DisputeSubmission>(
      context: context,
      isScrollControlled: true,
      backgroundColor: Colors.white,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (_) => DisputeFormSheet(rideId: rideId),
    );
  }

  @override
  State<DisputeFormSheet> createState() => _DisputeFormSheetState();
}

class _DisputeFormSheetState extends State<DisputeFormSheet> {
  static const int _maxDescriptionLength = 1000;
  static const int _maxEvidenceFiles = 3;

  final _formKey = GlobalKey<FormState>();
  final _descriptionController = TextEditingController();

  DisputeReason? _selectedReason;
  final List<File> _evidenceFiles = [];
  bool _submitting = false;

  @override
  void dispose() {
    _descriptionController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    // Pad by the keyboard height so the submit button stays visible.
    final keyboardInset = MediaQuery.of(context).viewInsets.bottom;
    return Padding(
      padding: EdgeInsets.only(left: 16, right: 16, top: 16, bottom: keyboardInset + 16),
      child: Form(
        key: _formKey,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  const Icon(Icons.gavel, color: AppColors.error),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Text(
                      'Dispute ride ${_shortId(widget.rideId)}',
                      style: const TextStyle(
                          fontSize: 16, fontWeight: FontWeight.bold),
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 4),
              Text(
                'Disputes must be filed within 72 hours of ride completion. '
                'Simple cases (no-show) are refunded automatically.',
                style: TextStyle(fontSize: 12, color: Colors.grey[600]),
              ),
              const SizedBox(height: 16),
              // ---- Reason dropdown ----
              DropdownButtonFormField<DisputeReason>(
                decoration: const InputDecoration(
                  labelText: 'Reason',
                  border: OutlineInputBorder(),
                  contentPadding:
                      EdgeInsets.symmetric(horizontal: 12, vertical: 14),
                ),
                value: _selectedReason,
                items: DisputeReason.reasons
                    .map(
                      (r) => DropdownMenuItem(
                        value: r,
                        child: Text(r.label, overflow: TextOverflow.ellipsis),
                      ),
                    )
                    .toList(),
                validator: (v) => v == null ? 'Select a reason' : null,
                onChanged: (v) => setState(() => _selectedReason = v),
              ),
              const SizedBox(height: 16),
              // ---- Description ----
              TextFormField(
                controller: _descriptionController,
                maxLines: 4,
                maxLength: _maxDescriptionLength,
                decoration: const InputDecoration(
                  labelText: 'What happened?',
                  alignLabelWithHint: true,
                  border: OutlineInputBorder(),
                  contentPadding:
                      EdgeInsets.symmetric(horizontal: 12, vertical: 12),
                ),
                validator: (v) {
                  final text = v?.trim() ?? '';
                  if (text.length < 10) {
                    return 'Please describe the issue (min 10 characters)';
                  }
                  return null;
                },
              ),
              const SizedBox(height: 8),
              // ---- Evidence picker ----
              _buildEvidenceSection(),
              const SizedBox(height: 20),
              // ---- Submit ----
              SizedBox(
                width: double.infinity,
                height: 48,
                child: ElevatedButton.icon(
                  style: ElevatedButton.styleFrom(
                    backgroundColor: AppColors.error,
                    foregroundColor: Colors.white,
                    disabledBackgroundColor: Colors.grey[300],
                  ),
                  onPressed: _submitting ? null : _onSubmit,
                  icon: _submitting
                      ? const SizedBox(
                          width: 18,
                          height: 18,
                          child: CircularProgressIndicator(
                              strokeWidth: 2, color: Colors.white),
                        )
                      : const Icon(Icons.send),
                  label: Text(_submitting
                      ? 'Submitting…'
                      : 'Submit dispute'),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildEvidenceSection() {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          mainAxisAlignment: MainAxisAlignment.spaceBetween,
          children: [
            Text(
              'Evidence (photos, max $_maxEvidenceFiles)',
              style: const TextStyle(fontWeight: FontWeight.w600),
            ),
            if (_evidenceFiles.length < _maxEvidenceFiles)
              TextButton.icon(
                onPressed: _pickEvidence,
                icon: const Icon(Icons.add_a_photo_outlined, size: 18),
                label: const Text('Add photo'),
              ),
          ],
        ),
        if (_evidenceFiles.isEmpty)
          Text(
            'Optional — receipts or photos help our team resolve faster.',
            style: TextStyle(fontSize: 12, color: Colors.grey[600]),
          )
        else
          SizedBox(
            height: 84,
            child: ListView.separated(
              scrollDirection: Axis.horizontal,
              itemCount: _evidenceFiles.length,
              separatorBuilder: (_, __) => const SizedBox(width: 8),
              itemBuilder: (context, index) {
                final file = _evidenceFiles[index];
                return Stack(
                  children: [
                    ClipRRect(
                      borderRadius: BorderRadius.circular(8),
                      child: Image.file(
                        file,
                        width: 84,
                        height: 84,
                        fit: BoxFit.cover,
                        errorBuilder: (_, __, ___) => Container(
                          width: 84,
                          height: 84,
                          color: Colors.grey[200],
                          child: const Icon(Icons.broken_image_outlined),
                        ),
                      ),
                    ),
                    Positioned(
                      top: 2,
                      right: 2,
                      child: GestureDetector(
                        onTap: () =>
                            setState(() => _evidenceFiles.removeAt(index)),
                        child: CircleAvatar(
                          radius: 10,
                          backgroundColor: Colors.black54,
                          child: const Icon(Icons.close,
                              size: 14, color: Colors.white),
                        ),
                      ),
                    ),
                  ],
                );
              },
            ),
          ),
      ],
    );
  }

  /// Placeholder for image_picker integration. The actual file selection is
  /// wired through the platform channel in the app shell; here we record a
  /// local path stub so the flow and payload remain testable end-to-end.
  Future<void> _pickEvidence() async {
    // TODO(phase-h-follow-up): replace with
    //   ImagePicker().pickImage(source: ImageSource.gallery)
    // once the image_picker dependency is added to pubspec.yaml.
    setState(() {
      _evidenceFiles.add(File('/pending-evidence/${DateTime.now().millisecondsSinceEpoch}.jpg'));
    });
  }

  void _onSubmit() {
    if (!(_formKey.currentState?.validate() ?? false)) return;
    final reason = _selectedReason;
    if (reason == null) return;

    setState(() => _submitting = true);
    Navigator.of(context).pop(
      DisputeSubmission(
        reasonCode: reason.code,
        description: _descriptionController.text.trim(),
        // Local paths are uploaded by the repository layer before the
        // POST /disputes call; the API only stores the resulting URLs.
        evidenceUrls: _evidenceFiles.map((f) => f.path).toList(),
      ),
    );
  }

  String _shortId(String id) =>
      id.length <= 8 ? id : '${id.substring(id.length - 8)}';
}
