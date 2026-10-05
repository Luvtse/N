import 'package:hive/hive.dart';

import '../../../../core/network/api_client.dart';
import '../../domain/entities/driver.dart';

abstract class AuthRepository {
  Future<Driver> login(String email, String password);
  Future<Driver> register(Map<String, dynamic> data);
  Future<void> logout();
  Future<Driver?> getCurrentDriver();
  Future<bool> isAuthenticated();
  Future<String?> getAccessToken();
  Future<void> updateDriverStatus(String status);
}

class AuthRepositoryImpl implements AuthRepository {
  final ApiClient _apiClient;
  static const String _authBoxName = 'driver_auth';

  AuthRepositoryImpl({required ApiClient apiClient}) : _apiClient = apiClient;

  Future<Box> _getAuthBox() async {
    return await Hive.openBox(_authBoxName);
  }

  @override
  Future<Driver> login(String email, String password) async {
    try {
      final response = await _apiClient.post(
        '/api/v1/auth/login',
        data: {
          'email': email,
          'password': password,
          'role': 'driver',
        },
      );

      final data = response.data as Map<String, dynamic>;
      
      // Save tokens
      final authBox = await _getAuthBox();
      await authBox.put('access_token', data['access_token']);
      await authBox.put('refresh_token', data['refresh_token']);
      await authBox.put('token_expires_at', data['expires_in']);
      await authBox.put('base_url', _apiClient.baseUrl);

      // Get driver profile
      final driverData = data['user'] as Map<String, dynamic>;
      final driverResponse = await _apiClient.get('/api/v1/drivers/profile');
      final driverProfile = driverResponse.data as Map<String, dynamic>;

      return Driver.fromJson({
        ...driverData,
        ...driverProfile,
      });
    } catch (e) {
      throw Exception('Login failed: ${e.toString()}');
    }
  }

  @override
  Future<Driver> register(Map<String, dynamic> data) async {
    try {
      final response = await _apiClient.post(
        '/api/v1/auth/register',
        data: {
          ...data,
          'role': 'driver',
        },
      );

      final responseData = response.data as Map<String, dynamic>;
      
      // Save tokens
      final authBox = await _getAuthBox();
      await authBox.put('access_token', responseData['access_token']);
      await authBox.put('refresh_token', responseData['refresh_token']);
      await authBox.put('token_expires_at', responseData['expires_in']);
      await authBox.put('base_url', _apiClient.baseUrl);

      // Get driver profile
      final driverData = responseData['user'] as Map<String, dynamic>;
      return Driver.fromJson(driverData);
    } catch (e) {
      throw Exception('Registration failed: ${e.toString()}');
    }
  }

  @override
  Future<void> logout() async {
    try {
      await _apiClient.post('/api/v1/auth/logout');
    } catch (e) {
      // Ignore logout errors
    } finally {
      final authBox = await _getAuthBox();
      await authBox.clear();
    }
  }

  @override
  Future<Driver?> getCurrentDriver() async {
    try {
      final authBox = await _getAuthBox();
      final token = authBox.get('access_token');
      
      if (token == null) return null;

      final response = await _apiClient.get('/api/v1/drivers/profile');
      final data = response.data as Map<String, dynamic>;
      
      return Driver.fromJson(data);
    } catch (e) {
      return null;
    }
  }

  @override
  Future<bool> isAuthenticated() async {
    final authBox = await _getAuthBox();
    final token = authBox.get('access_token');
    final expiresAt = authBox.get('token_expires_at');
    
    if (token == null || expiresAt == null) return false;
    
    final expiry = DateTime.fromMillisecondsSinceEpoch(expiresAt);
    return expiry.isAfter(DateTime.now());
  }

  @override
  Future<String?> getAccessToken() async {
    final authBox = await _getAuthBox();
    return authBox.get('access_token') as String?;
  }

  @override
  Future<void> updateDriverStatus(String status) async {
    try {
      await _apiClient.patch(
        '/api/v1/drivers/status',
        data: {'status': status},
      );
    } catch (e) {
      throw Exception('Failed to update status: ${e.toString()}');
    }
  }
}