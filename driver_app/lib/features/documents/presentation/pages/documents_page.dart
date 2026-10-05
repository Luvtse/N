import 'dart:io';
import 'package:flutter/material.dart';
import 'package:image_picker/image_picker.dart';

import '../../../../core/theme/app_colors.dart';

class DocumentsPage extends StatefulWidget {
  const DocumentsPage({super.key});

  @override
  State<DocumentsPage> createState() => _DocumentsPageState();
}

class _DocumentsPageState extends State<DocumentsPage> {
  final _picker = ImagePicker();
  
  final List<Map<String, dynamic>> _documents = [
    {
      'type': 'Driver License',
      'status': 'verified',
      'expiryDate': '2028-12-31',
      'icon': Icons.badge,
    },
    {
      'type': 'Insurance',
      'status': 'verified',
      'expiryDate': '2027-06-30',
      'icon': Icons.shield,
    },
    {
      'type': 'Vehicle Registration',
      'status': 'pending',
      'expiryDate': '2027-03-15',
      'icon': Icons.car_repair,
    },
    {
      'type': 'Background Check',
      'status': 'verified',
      'expiryDate': '2027-01-01',
      'icon': Icons.verified_user,
    },
  ];

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Documents'),
      ),
      body: ListView.builder(
        padding: const EdgeInsets.all(16),
        itemCount: _documents.length,
        itemBuilder: (context, index) {
          final doc = _documents[index];
          return _buildDocumentCard(doc);
        },
      ),
    );
  }

  Widget _buildDocumentCard(Map<String, dynamic> doc) {
    final status = doc['status'] as String;
    final statusColor = status == 'verified'
        ? AppColors.success
        : status == 'pending'
            ? AppColors.warning
            : AppColors.error;

    return Card(
      margin: const EdgeInsets.only(bottom: 12),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(
                  doc['icon'] as IconData,
                  color: AppColors.primary,
                  size: 32,
                ),
                const SizedBox(width: 16),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        doc['type'] as String,
                        style: const TextStyle(
                          fontSize: 18,
                          fontWeight: FontWeight.bold,
                        ),
                      ),
                      const SizedBox(height: 4),
                      Text(
                        'Expires: ${doc['expiryDate']}',
                        style: TextStyle(
                          fontSize: 12,
                          color: Colors.grey[600],
                        ),
                      ),
                    ],
                  ),
                ),
                Container(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 12,
                    vertical: 6,
                  ),
                  decoration: BoxDecoration(
                    color: statusColor.withOpacity(0.1),
                    borderRadius: BorderRadius.circular(12),
                  ),
                  child: Text(
                    status.toUpperCase(),
                    style: TextStyle(
                      color: statusColor,
                      fontSize: 12,
                      fontWeight: FontWeight.bold,
                    ),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 16),
            Row(
              children: [
                Expanded(
                  child: OutlinedButton.icon(
                    onPressed: () => _viewDocument(doc),
                    icon: const Icon(Icons.visibility),
                    label: const Text('View'),
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: ElevatedButton.icon(
                    onPressed: () => _uploadDocument(doc),
                    icon: const Icon(Icons.upload),
                    label: const Text('Upload'),
                    style: ElevatedButton.styleFrom(
                      backgroundColor: AppColors.primary,
                    ),
                  ),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }

  void _viewDocument(Map<String, dynamic> doc) {
    // TODO: Open document viewer
  }

  Future<void> _uploadDocument(Map<String, dynamic> doc) async {
    final image = await _picker.pickImage(
      source: ImageSource.gallery,
      maxWidth: 1024,
      maxHeight: 1024,
    );

    if (image != null) {
      // TODO: Upload to backend
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text('${doc['type']} uploaded successfully'),
          backgroundColor: AppColors.success,
        ),
      );
    }
  }
}