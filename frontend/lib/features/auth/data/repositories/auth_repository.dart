import 'dart:convert';

import 'package:flutter/foundation.dart';

import '../../../../core/network/api_client.dart';

// ============================================================================
// ABSTRACT INTERFACE
// ============================================================================

/// Abstract repository interface for authentication operations
abstract class AuthRepository {
  /// Login with email and password
  Future<AuthResult> login({
    required String email,
    required String password,
  });

  /// Register a new user
  Future<AuthResult> register({
    required String email,
    required String password,
    required String fullName,
    required String phone,
  });

  /// Logout the current user
  Future<void> logout();

  /// Check if user has stored tokens
  Future<bool> hasStoredTokens();

  /// Validate the current access token
  Future<bool> validateAccessToken();

  /// Refresh the access token using refresh token
  Future<bool> refreshAccessToken();

  /// Get the current user profile
  Future<User?> getCurrentUser();

  /// Get the current access token
  Future<String?> getAccessToken();

  /// Get token expiration time
  Future<DateTime?> getTokenExpiration();

  /// Check if onboarding is complete
  Future<bool> isOnboardingComplete();

  /// Mark onboarding as complete
  Future<void> markOnboardingComplete();

  /// Clear all stored tokens
  Future<void> clearStoredTokens();
}

// ============================================================================
// DOMAIN ENTITIES
// ============================================================================

/// User entity
class User {
  final String id;
  final String email;
  final String fullName;
  final String phone;
  final String? profileImageUrl;
  final String role;
  final DateTime createdAt;

  const User({
    required this.id,
    required this.email,
    required this.fullName,
    required this.phone,
    this.profileImageUrl,
    required this.role,
    required this.createdAt,
  });

  factory User.fromJson(Map<String, dynamic> json) {
    return User(
      id: json['id'] as String,
      email: json['email'] as String,
      fullName: json['full_name'] as String,
      phone: json['phone'] as String,
      profileImageUrl: json['profile_image_url'] as String?,
      role: json['role'] as String? ?? 'rider',
      createdAt: DateTime.parse(json['created_at'] as String),
    );
  }

  Map<String, dynamic> toJson() {
    return {
      'id': id,
      'email': email,
      'full_name': fullName,
      'phone': phone,
      'profile_image_url': profileImageUrl,
      'role': role,
      'created_at': createdAt.toIso8601String(),
    };
  }

  User copyWith({
    String? email,
    String? fullName,
    String? phone,
    String? profileImageUrl,
    String? role,
  }) {
    return User(
      id: id,
      email: email ?? this.email,
      fullName: fullName ?? this.fullName,
      phone: phone ?? this.phone,
      profileImageUrl: profileImageUrl ?? this.profileImageUrl,
      role: role ?? this.role,
      createdAt: createdAt,
    );
  }
}

/// Auth result from login/register
class AuthResult {
  final User? user;
  final String? error;
  final String? errorCode;

  const AuthResult._({this.user, this.error, this.errorCode});

  const AuthResult.success(User user) : this._(user: user);
  const AuthResult.failure({required String error, String? errorCode})
      : this._(error: error, errorCode: errorCode);

  bool get isSuccess => user != null;
  bool get isFailure => error != null;
}

// ============================================================================
// TOKEN STORAGE INTERFACE
// ============================================================================

/// Interface for secure token storage
abstract class TokenStorage {
  Future<String?> getAccessToken();
  Future<String?> getRefreshToken();
  Future<void> saveTokens({
    required String accessToken,
    required String refreshToken,
  });
  Future<void> clearTokens();
  Future<DateTime?> getTokenExpiration();
  Future<void> saveTokenExpiration(DateTime expiresAt);
}

// ============================================================================
// IMPLEMENTATION
// ============================================================================

/// Concrete implementation of AuthRepository
class AuthRepositoryImpl implements AuthRepository {
  final ApiClient _apiClient;
  final TokenStorage _tokenStorage;

  // In-memory cache for current user
  User? _cachedUser;
  bool? _onboardingComplete;

  AuthRepositoryImpl({
    required ApiClient apiClient,
    required TokenStorage tokenStorage,
  })  : _apiClient = apiClient,
        _tokenStorage = tokenStorage {
    // Set up unauthorized callback for token refresh
    _apiClient.onUnauthorized = _handleUnauthorized;
  }

  // ==========================================================================
  // LOGIN / REGISTER
  // ==========================================================================

  @override
  Future<AuthResult> login({
    required String email,
    required String password,
  }) async {
    try {
      final response = await _apiClient.post(
        '/api/v1/auth/login',
        data: {
          'email': email,
          'password': password,
        },
      );

      final data = response.data as Map<String, dynamic>;
      
      // Extract tokens
      final accessToken = data['access_token'] as String;
      final refreshToken = data['refresh_token'] as String;
      final expiresIn = data['expires_in'] as int;

      // Save tokens
      await _tokenStorage.saveTokens(
        accessToken: accessToken,
        refreshToken: refreshToken,
      );

      // Save expiration
      final expiresAt = DateTime.now().add(Duration(seconds: expiresIn));
      await _tokenStorage.saveTokenExpiration(expiresAt);

      // Extract user
      final userData = data['user'] as Map<String, dynamic>;
      final user = User.fromJson(userData);
      _cachedUser = user;

      return AuthResult.success(user);
    } on UnauthorizedException catch (e) {
      return AuthResult.failure(
        error: e.message,
        errorCode: e.errorCode,
      );
    } on ApiException catch (e) {
      return AuthResult.failure(
        error: e.message,
        errorCode: e.errorCode,
      );
    } catch (e) {
      return AuthResult.failure(
        error: 'Login failed: ${e.toString()}',
        errorCode: 'UNKNOWN_ERROR',
      );
    }
  }

  @override
  Future<AuthResult> register({
    required String email,
    required String password,
    required String fullName,
    required String phone,
  }) async {
    try {
      final response = await _apiClient.post(
        '/api/v1/auth/register',
        data: {
          'email': email,
          'password': password,
          'full_name': fullName,
          'phone': phone,
        },
      );

      final data = response.data as Map<String, dynamic>;
      
      final accessToken = data['access_token'] as String;
      final refreshToken = data['refresh_token'] as String;
      final expiresIn = data['expires_in'] as int;

      await _tokenStorage.saveTokens(
        accessToken: accessToken,
        refreshToken: refreshToken,
      );

      final expiresAt = DateTime.now().add(Duration(seconds: expiresIn));
      await _tokenStorage.saveTokenExpiration(expiresAt);

      final userData = data['user'] as Map<String, dynamic>;
      final user = User.fromJson(userData);
      _cachedUser = user;

      // New users haven't completed onboarding
      _onboardingComplete = false;

      return AuthResult.success(user);
    } on ConflictException catch (e) {
      return AuthResult.failure(
        error: 'An account with this email already exists',
        errorCode: 'EMAIL_EXISTS',
      );
    } on ValidationException catch (e) {
      return AuthResult.failure(
        error: e.message,
        errorCode: e.errorCode,
      );
    } on ApiException catch (e) {
      return AuthResult.failure(
        error: e.message,
        errorCode: e.errorCode,
      );
    } catch (e) {
      return AuthResult.failure(
        error: 'Registration failed: ${e.toString()}',
        errorCode: 'UNKNOWN_ERROR',
      );
    }
  }

  // ==========================================================================
  // LOGOUT
  // ==========================================================================

  @override
  Future<void> logout() async {
    try {
      // Try to call logout endpoint (best effort)
      await _apiClient.post('/api/v1/auth/logout').catchError((_) {});
    } finally {
      // Always clear local state
      await _tokenStorage.clearTokens();
      _cachedUser = null;
      _onboardingComplete = null;
    }
  }

  // ==========================================================================
  // TOKEN MANAGEMENT
  // ==========================================================================

  @override
  Future<bool> hasStoredTokens() async {
    final accessToken = await _tokenStorage.getAccessToken();
    return accessToken != null && accessToken.isNotEmpty;
  }

  @override
  Future<bool> validateAccessToken() async {
    try {
      final token = await _tokenStorage.getAccessToken();
      if (token == null) return false;

      // Call a protected endpoint to validate
      await _apiClient.get('/api/v1/auth/me');
      return true;
    } catch (e) {
      return false;
    }
  }

  @override
  Future<bool> refreshAccessToken() async {
    try {
      final refreshToken = await _tokenStorage.getRefreshToken();
      if (refreshToken == null) return false;

      final response = await _apiClient.post(
        '/api/v1/auth/refresh',
        data: {'refresh_token': refreshToken},
      );

      final data = response.data as Map<String, dynamic>;
      
      final newAccessToken = data['access_token'] as String;
      final newRefreshToken = data['refresh_token'] as String;
      final expiresIn = data['expires_in'] as int;

      await _tokenStorage.saveTokens(
        accessToken: newAccessToken,
        refreshToken: newRefreshToken,
      );

      final expiresAt = DateTime.now().add(Duration(seconds: expiresIn));
      await _tokenStorage.saveTokenExpiration(expiresAt);

      return true;
    } catch (e) {
      debugPrint('Token refresh failed: $e');
      return false;
    }
  }

  @override
  Future<String?> getAccessToken() async {
    return await _tokenStorage.getAccessToken();
  }

  @override
  Future<DateTime?> getTokenExpiration() async {
    return await _tokenStorage.getTokenExpiration();
  }

  @override
  Future<void> clearStoredTokens() async {
    await _tokenStorage.clearTokens();
    _cachedUser = null;
  }

  // ==========================================================================
  // USER PROFILE
  // ==========================================================================

  @override
  Future<User?> getCurrentUser() async {
    // Return cached user if available
    if (_cachedUser != null) {
      return _cachedUser;
    }

    try {
      final response = await _apiClient.get('/api/v1/auth/me');
      final data = response.data as Map<String, dynamic>;
      final user = User.fromJson(data['user'] as Map<String, dynamic>);
      _cachedUser = user;
      return user;
    } catch (e) {
      debugPrint('Failed to get current user: $e');
      return null;
    }
  }

  // ==========================================================================
  // ONBOARDING
  // ==========================================================================

  @override
  Future<bool> isOnboardingComplete() async {
    if (_onboardingComplete != null) {
      return _onboardingComplete!;
    }

    try {
      final response = await _apiClient.get('/api/v1/auth/onboarding-status');
      final data = response.data as Map<String, dynamic>;
      _onboardingComplete = data['complete'] as bool? ?? false;
      return _onboardingComplete!;
    } catch (e) {
      debugPrint('Failed to check onboarding status: $e');
      return false;
    }
  }

  @override
  Future<void> markOnboardingComplete() async {
    try {
      await _apiClient.post('/api/v1/auth/onboarding/complete');
      _onboardingComplete = true;
    } catch (e) {
      debugPrint('Failed to mark onboarding complete: $e');
      rethrow;
    }
  }

  // ==========================================================================
  // TOKEN REFRESH HANDLER
  // ==========================================================================

  /// Called by ApiClient when a 401 is received
  Future<void> _handleUnauthorized() async {
    debugPrint('Unauthorized - attempting token refresh');
    final refreshed = await refreshAccessToken();
    if (!refreshed) {
      // Refresh failed, force logout
      await logout();
    }
  }
}