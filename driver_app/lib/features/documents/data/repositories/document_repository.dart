import 'dart:io';

import '../../../../core/network/api_client.dart';
import '../../domain/entities/driver_document.dart';

abstract class DocumentRepository {
  Future<List<DriverDocument>> getDocuments();
  Future<DriverDocument> uploadDocument(String type, File file, Map<String, dynamic> metadata);
  Future<void> deleteDocument(String documentId);
}

class DocumentRepositoryImpl implements DocumentRepository {
  final ApiClient _apiClient;

  DocumentRepositoryImpl({required ApiClient apiClient}) : _apiClient = apiClient;

  @override
  Future<List<DriverDocument>> getDocuments() async {
    try {
      final response = await _apiClient.get('/api/v1/drivers/documents');
      final data = response.data as Map<String, dynamic>;
      final documents = data['documents'] as List;
      return documents
          .map((e) => DriverDocument.fromJson(e as Map<String, dynamic>))
          .toList();
    } catch (e) {
      throw Exception('Failed to get documents: ${e.toString()}');
    }
  }

  @override
  Future<DriverDocument> uploadDocument(
    String type,
    File file,
    Map<String, dynamic> metadata,
  ) async {
    try {
      final formData = {
        'type': type,
        'file': await file.readAsBytes(),
        ...metadata,
      };

      final response = await _apiClient.post(
        '/api/v1/drivers/documents/upload',
        data: formData,
      );
      final data = response.data as Map<String, dynamic>;
      return DriverDocument.fromJson(data);
    } catch (e) {
      throw Exception('Failed to upload document: ${e.toString()}');
    }
  }

  @override
  Future<void> deleteDocument(String documentId) async {
    try {
      await _apiClient.delete('/api/v1/drivers/documents/$documentId');
    } catch (e) {
      throw Exception('Failed to delete document: ${e.toString()}');
    }
  }
}