import 'dart:io';

import '../../../../core/network/api_client.dart';
import '../../domain/entities/driver_profile.dart';

abstract class DriverProfileRepository {
  Future<DriverProfile> getProfile();
  Future<DriverProfile> updateProfile(Map<String, dynamic> data);
  Future<void> updateVehicleInfo(Map<String, dynamic> data);
  Future<String?> uploadProfilePicture(File file);
}

class DriverProfileRepositoryImpl implements DriverProfileRepository {
  final ApiClient _apiClient;

  DriverProfileRepositoryImpl({required ApiClient apiClient}) : _apiClient = apiClient;

  @override
  Future<DriverProfile> getProfile() async {
    try {
      final response = await _apiClient.get('/api/v1/drivers/profile');
      final data = response.data as Map<String, dynamic>;
      return DriverProfile.fromJson(data);
    } catch (e) {
      throw Exception('Failed to get profile: ${e.toString()}');
    }
  }

  @override
  Future<DriverProfile> updateProfile(Map<String, dynamic> data) async {
    try {
      final response = await _apiClient.patch(
        '/api/v1/drivers/profile',
        data: data,
      );
      final responseData = response.data as Map<String, dynamic>;
      return DriverProfile.fromJson(responseData);
    } catch (e) {
      throw Exception('Failed to update profile: ${e.toString()}');
    }
  }

  @override
  Future<void> updateVehicleInfo(Map<String, dynamic> data) async {
    try {
      await _apiClient.patch(
        '/api/v1/drivers/vehicle',
        data: data,
      );
    } catch (e) {
      throw Exception('Failed to update vehicle info: ${e.toString()}');
    }
  }

  @override
  Future<String?> uploadProfilePicture(File file) async {
    try {
      final response = await _apiClient.post(
        '/api/v1/drivers/profile/picture',
        data: {'file': await file.readAsBytes()},
      );
      final data = response.data as Map<String, dynamic>;
      return data['url'] as String?;
    } catch (e) {
      throw Exception('Failed to upload profile picture: ${e.toString()}');
    }
  }
}