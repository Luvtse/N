import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';
import 'package:pretty_dio_logger/pretty_dio_logger.dart';

// ============================================================================
// EXCEPTIONS (Typed, with error codes)
// ============================================================================

/// Base exception for all API errors
abstract class ApiException implements Exception {
  final String message;
  final int? statusCode;
  final String? errorCode;
  final dynamic responseData;

  const ApiException({
    required this.message,
    this.statusCode,
    this.errorCode,
    this.responseData,
  });

  @override
  String toString() => 'ApiException($statusCode): $message';
}

/// 400 Bad Request
class BadRequestException extends ApiException {
  const BadRequestException({
    required super.message,
    super.statusCode = 400,
    super.errorCode,
    super.responseData,
  });
}

/// 401 Unauthorized
class UnauthorizedException extends ApiException {
  const UnauthorizedException({
    required super.message,
    super.statusCode = 401,
    super.errorCode = 'UNAUTHORIZED',
    super.responseData,
  });
}

/// 403 Forbidden
class ForbiddenException extends ApiException {
  const ForbiddenException({
    required super.message,
    super.statusCode = 403,
    super.errorCode = 'FORBIDDEN',
    super.responseData,
  });
}

/// 404 Not Found
class NotFoundException extends ApiException {
  const NotFoundException({
    required super.message,
    super.statusCode = 404,
    super.errorCode = 'NOT_FOUND',
    super.responseData,
  });
}

/// 409 Conflict (e.g., consent already given)
class ConflictException extends ApiException {
  const ConflictException({
    required super.message,
    super.statusCode = 409,
    super.errorCode = 'CONFLICT',
    super.responseData,
  });
}

/// 422 Validation Error
class ValidationException extends ApiException {
  final Map<String, List<String>>? fieldErrors;

  const ValidationException({
    required super.message,
    super.statusCode = 422,
    super.errorCode = 'VALIDATION_ERROR',
    super.responseData,
    this.fieldErrors,
  });
}

/// 429 Rate Limited
class RateLimitException extends ApiException {
  final Duration? retryAfter;

  const RateLimitException({
    required super.message,
    super.statusCode = 429,
    super.errorCode = 'RATE_LIMITED',
    super.responseData,
    this.retryAfter,
  });
}

/// 5xx Server Error
class ServerException extends ApiException {
  const ServerException({
    required super.message,
    super.statusCode,
    super.errorCode = 'SERVER_ERROR',
    super.responseData,
  });
}

/// Network connectivity error
class NetworkException extends ApiException {
  const NetworkException({
    required super.message,
    super.errorCode = 'NETWORK_ERROR',
  });
}

/// Request timeout
class TimeoutException extends ApiException {
  const TimeoutException({
    required super.message,
    super.errorCode = 'TIMEOUT',
  });
}

// ============================================================================
// API CLIENT CONFIGURATION
// ============================================================================

class ApiClientConfig {
  final String baseUrl;
  final Duration connectTimeout;
  final Duration receiveTimeout;
  final Duration sendTimeout;
  final Map<String, String> defaultHeaders;
  final bool enableLogging;
  final int maxRetries;

  const ApiClientConfig({
    required this.baseUrl,
    this.connectTimeout = const Duration(seconds: 15),
    this.receiveTimeout = const Duration(seconds: 30),
    this.sendTimeout = const Duration(seconds: 15),
    this.defaultHeaders = const {
      'Content-Type': 'application/json',
      'Accept': 'application/json',
    },
    this.enableLogging = kDebugMode,
    this.maxRetries = 3,
  });
}

// ============================================================================
// TOKEN STORAGE INTERFACE (For auth interceptor)
// ============================================================================

abstract class TokenStorage {
  Future<String?> getAccessToken();
  Future<String?> getRefreshToken();
  Future<void> saveTokens({
    required String accessToken,
    required String refreshToken,
  });
  Future<void> clearTokens();
}

// ============================================================================
// API CLIENT
// ============================================================================

class ApiClient {
  late final Dio _dio;
  final ApiClientConfig _config;
  final TokenStorage? _tokenStorage;

  // Callback for token refresh (set by auth layer)
  Future<void> Function()? onUnauthorized;

  ApiClient({
    required ApiClientConfig config,
    TokenStorage? tokenStorage,
  })  : _config = config,
        _tokenStorage = tokenStorage {
    _initializeDio();
  }

  void _initializeDio() {
    _dio = Dio(BaseOptions(
      baseUrl: _config.baseUrl,
      connectTimeout: _config.connectTimeout,
      receiveTimeout: _config.receiveTimeout,
      sendTimeout: _config.sendTimeout,
      headers: _config.defaultHeaders,
      validateStatus: (status) => status != null && status < 500,
    ));

    // Add interceptors in order
    _dio.interceptors.addAll([
      _AuthInterceptor(this),
      _RetryInterceptor(
        dio: _dio,
        maxRetries: _config.maxRetries,
      ),
      if (_config.enableLogging)
        PrettyDioLogger(
          requestHeader: true,
          requestBody: true,
          responseBody: true,
          responseHeader: false,
          error: true,
          compact: false,
        ),
      _ErrorInterceptor(this),
    ]);
  }

  // ==========================================================================
  // HTTP METHODS
  // ==========================================================================

  Future<Response<T>> get<T>(
    String path, {
    Map<String, dynamic>? queryParameters,
    Options? options,
    CancelToken? cancelToken,
  }) async {
    try {
      return await _dio.get<T>(
        path,
        queryParameters: queryParameters,
        options: options,
        cancelToken: cancelToken,
      );
    } on DioException catch (e) {
      _handleDioError(e);
      rethrow;
    }
  }

  Future<Response<T>> post<T>(
    String path, {
    dynamic data,
    Map<String, dynamic>? queryParameters,
    Options? options,
    CancelToken? cancelToken,
  }) async {
    try {
      return await _dio.post<T>(
        path,
        data: data,
        queryParameters: queryParameters,
        options: options,
        cancelToken: cancelToken,
      );
    } on DioException catch (e) {
      _handleDioError(e);
      rethrow;
    }
  }

  Future<Response<T>> put<T>(
    String path, {
    dynamic data,
    Map<String, dynamic>? queryParameters,
    Options? options,
    CancelToken? cancelToken,
  }) async {
    try {
      return await _dio.put<T>(
        path,
        data: data,
        queryParameters: queryParameters,
        options: options,
        cancelToken: cancelToken,
      );
    } on DioException catch (e) {
      _handleDioError(e);
      rethrow;
    }
  }

  Future<Response<T>> patch<T>(
    String path, {
    dynamic data,
    Map<String, dynamic>? queryParameters,
    Options? options,
    CancelToken? cancelToken,
  }) async {
    try {
      return await _dio.patch<T>(
        path,
        data: data,
        queryParameters: queryParameters,
        options: options,
        cancelToken: cancelToken,
      );
    } on DioException catch (e) {
      _handleDioError(e);
      rethrow;
    }
  }

  Future<Response<T>> delete<T>(
    String path, {
    dynamic data,
    Map<String, dynamic>? queryParameters,
    Options? options,
    CancelToken? cancelToken,
  }) async {
    try {
      return await _dio.delete<T>(
        path,
        data: data,
        queryParameters: queryParameters,
        options: options,
        cancelToken: cancelToken,
      );
    } on DioException catch (e) {
      _handleDioError(e);
      rethrow;
    }
  }

  // ==========================================================================
  // ERROR HANDLING
  // ==========================================================================

  Never _handleDioError(DioException error) {
    switch (error.type) {
      case DioExceptionType.connectionTimeout:
      case DioExceptionType.sendTimeout:
      case DioExceptionType.receiveTimeout:
        throw const TimeoutException(message: 'Request timed out');

      case DioExceptionType.connectionError:
        throw const NetworkException(message: 'No internet connection');

      case DioExceptionType.badResponse:
        _handleBadResponse(error.response!);

      case DioExceptionType.cancel:
        throw const ApiException(message: 'Request cancelled')
            as Never;

      default:
        throw NetworkException(
          message: error.message ?? 'Unknown network error',
        );
    }
  }

  Never _handleBadResponse(Response response) {
    final data = response.data;
    final message = _extractErrorMessage(data) ?? 'Request failed';
    final errorCode = _extractErrorCode(data);

    switch (response.statusCode) {
      case 400:
        throw BadRequestException(
          message: message,
          errorCode: errorCode,
          responseData: data,
        );
      case 401:
        // Trigger token refresh callback
        onUnauthorized?.call();
        throw UnauthorizedException(
          message: message,
          responseData: data,
        );
      case 403:
        throw ForbiddenException(
          message: message,
          responseData: data,
        );
      case 404:
        throw NotFoundException(
          message: message,
          responseData: data,
        );
      case 409:
        throw ConflictException(
          message: message,
          responseData: data,
        );
      case 422:
        throw ValidationException(
          message: message,
          responseData: data,
          fieldErrors: _extractFieldErrors(data),
        );
      case 429:
        final retryAfter = response.headers.value('retry-after');
        throw RateLimitException(
          message: message,
          responseData: data,
          retryAfter: retryAfter != null
              ? Duration(seconds: int.tryParse(retryAfter) ?? 60)
              : null,
        );
      default:
        if (response.statusCode != null && response.statusCode! >= 500) {
          throw ServerException(
            message: message,
            statusCode: response.statusCode,
            responseData: data,
          );
        }
        throw ApiException(
          message: message,
          statusCode: response.statusCode,
          errorCode: errorCode,
          responseData: data,
        ) as Never;
    }
  }

  String? _extractErrorMessage(dynamic data) {
    if (data is Map<String, dynamic>) {
      return data['message'] as String? ??
          data['error'] as String? ??
          (data['errors'] as Map<String, dynamic>?)?.values.first as String?;
    }
    if (data is String) return data;
    return null;
  }

  String? _extractErrorCode(dynamic data) {
    if (data is Map<String, dynamic>) {
      return data['code'] as String? ?? data['error_code'] as String?;
    }
    return null;
  }

  Map<String, List<String>>? _extractFieldErrors(dynamic data) {
    if (data is Map<String, dynamic> && data['errors'] is Map) {
      final errors = <String, List<String>>{};
      (data['errors'] as Map).forEach((key, value) {
        if (value is List) {
          errors[key] = value.map((e) => e.toString()).toList();
        } else {
          errors[key] = [value.toString()];
        }
      });
      return errors;
    }
    return null;
  }

  // ==========================================================================
  // UTILITIES
  // ==========================================================================

  /// Get the underlying Dio instance (for advanced use cases)
  Dio get dio => _dio;

  /// Dispose resources
  void dispose() {
    _dio.close();
  }
}

// ============================================================================
// AUTH INTERCEPTOR (Attaches JWT, handles refresh)
// ============================================================================

class _AuthInterceptor extends Interceptor {
  final ApiClient _client;

  _AuthInterceptor(this._client);

  @override
  void onRequest(RequestOptions options, RequestInterceptorHandler handler) async {
    // Skip auth for public endpoints
    if (_isPublicEndpoint(options.path)) {
      return handler.next(options);
    }

    // Attach access token
    final token = await _client._tokenStorage?.getAccessToken();
    if (token != null && token.isNotEmpty) {
      options.headers['Authorization'] = 'Bearer $token';
    }

    handler.next(options);
  }

  bool _isPublicEndpoint(String path) {
    const publicPaths = [
      '/api/v1/auth/login',
      '/api/v1/auth/register',
      '/api/v1/auth/refresh',
      '/api/v1/legal/documents',
      '/health',
      '/ready',
    ];
    return publicPaths.any((p) => path.startsWith(p));
  }
}

// ============================================================================
// RETRY INTERCEPTOR (Exponential backoff for transient failures)
// ============================================================================

class _RetryInterceptor extends Interceptor {
  final Dio dio;
  final int maxRetries;

  _RetryInterceptor({required this.dio, this.maxRetries = 3});

  @override
  void onError(DioException err, ErrorInterceptorHandler handler) async {
    // Only retry on specific errors
    if (!_shouldRetry(err)) {
      return handler.next(err);
    }

    for (var attempt = 1; attempt <= maxRetries; attempt++) {
      // Exponential backoff: 1s, 2s, 4s
      final delay = Duration(seconds: pow(2, attempt - 1).toInt());
      await Future.delayed(delay);

      try {
        final response = await dio.request(
          err.requestOptions.path,
          data: err.requestOptions.data,
          queryParameters: err.requestOptions.queryParameters,
          options: Options(
            method: err.requestOptions.method,
            headers: err.requestOptions.headers,
          ),
        );
        return handler.resolve(response);
      } on DioException catch (e) {
        if (attempt == maxRetries || !_shouldRetry(e)) {
          return handler.next(e);
        }
      }
    }

    handler.next(err);
  }

  bool _shouldRetry(DioException error) {
    // Retry on network errors and 5xx server errors
    if (error.type == DioExceptionType.connectionError ||
        error.type == DioExceptionType.connectionTimeout ||
        error.type == DioExceptionType.receiveTimeout ||
        error.type == DioExceptionType.sendTimeout) {
      return true;
    }

    if (error.response != null) {
      final status = error.response!.statusCode;
      return status != null && status >= 500 && status != 501;
    }

    return false;
  }
}

// ============================================================================
// ERROR INTERCEPTOR (Centralized error transformation)
// ============================================================================

class _ErrorInterceptor extends Interceptor {
  final ApiClient _client;

  _ErrorInterceptor(this._client);

  @override
  void onResponse(Response response, ResponseInterceptorHandler handler) {
    // Let successful responses pass through
    handler.next(response);
  }
}