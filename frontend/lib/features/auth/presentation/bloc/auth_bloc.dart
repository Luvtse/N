import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:equatable/equatable.dart';

import '../../data/repositories/auth_repository.dart';
import '../../domain/entities/user.dart';
import '../../domain/usecases/login_usecase.dart';
import '../../domain/usecases/register_usecase.dart';

// ============================================================================
// EVENTS
// ============================================================================

abstract class AuthEvent extends Equatable {
  const AuthEvent();

  @override
  List<Object?> get props => [];
}

/// Check if user has a valid stored session
class CheckAuthStatus extends AuthEvent {
  const CheckAuthStatus();
}

/// Login with email and password
class LoginRequested extends AuthEvent {
  final String email;
  final String password;

  const LoginRequested({
    required this.email,
    required this.password,
  });

  @override
  List<Object?> get props => [email, password];
}

/// Register a new user
class RegisterRequested extends AuthEvent {
  final String email;
  final String password;
  final String fullName;
  final String phone;

  const RegisterRequested({
    required this.email,
    required this.password,
    required this.fullName,
    required this.phone,
  });

  @override
  List<Object?> get props => [email, password, fullName, phone];
}

/// Logout the current user
class LogoutRequested extends AuthEvent {
  const LogoutRequested();
}

/// Mark onboarding as complete (after legal consent)
class OnboardingCompleted extends AuthEvent {
  const OnboardingCompleted();
}

/// Refresh the access token using refresh token
class TokenRefreshRequested extends AuthEvent {
  const TokenRefreshRequested();
}

/// Update user profile
class ProfileUpdated extends AuthEvent {
  final User user;

  const ProfileUpdated(this.user);

  @override
  List<Object?> get props => [user];
}

// ============================================================================
// STATES
// ============================================================================

abstract class AuthState extends Equatable {
  const AuthState();

  @override
  List<Object?> get props => [];
}

/// Initial state before auth check
class AuthInitial extends AuthState {
  const AuthInitial();
}

/// Auth check in progress
class AuthLoading extends AuthState {
  const AuthLoading();
}

/// User is authenticated
class AuthAuthenticated extends AuthState {
  final User user;
  final bool onboardingComplete;
  final DateTime tokenExpiresAt;

  const AuthAuthenticated({
    required this.user,
    required this.onboardingComplete,
    required this.tokenExpiresAt,
  });

  AuthAuthenticated copyWith({
    User? user,
    bool? onboardingComplete,
    DateTime? tokenExpiresAt,
  }) {
    return AuthAuthenticated(
      user: user ?? this.user,
      onboardingComplete: onboardingComplete ?? this.onboardingComplete,
      tokenExpiresAt: tokenExpiresAt ?? this.tokenExpiresAt,
    );
  }

  @override
  List<Object?> get props => [user, onboardingComplete, tokenExpiresAt];
}

/// User is not authenticated
class AuthUnauthenticated extends AuthState {
  const AuthUnauthenticated();
}

/// Auth operation failed
class AuthError extends AuthState {
  final String message;
  final String? errorCode;

  const AuthError({
    required this.message,
    this.errorCode,
  });

  @override
  List<Object?> get props => [message, errorCode];
}

// ============================================================================
// BLOC
// ============================================================================

class AuthBloc extends Bloc<AuthEvent, AuthState> {
  final LoginUseCase _loginUseCase;
  final RegisterUseCase _registerUseCase;
  final AuthRepository _authRepository;

  /// ValueNotifier for GoRouter's refreshListenable
  /// This allows the router to react to auth state changes
  final ValueNotifier<AuthState> authNotifier = ValueNotifier<AuthState>(
    const AuthInitial(),
  );

  AuthBloc({
    required LoginUseCase loginUseCase,
    required RegisterUseCase registerUseCase,
    required AuthRepository authRepository,
  })  : _loginUseCase = loginUseCase,
        _registerUseCase = registerUseCase,
        _authRepository = authRepository,
        super(const AuthInitial()) {
    on<CheckAuthStatus>(_onCheckAuthStatus);
    on<LoginRequested>(_onLoginRequested);
    on<RegisterRequested>(_onRegisterRequested);
    on<LogoutRequested>(_onLogoutRequested);
    on<OnboardingCompleted>(_onOnboardingCompleted);
    on<TokenRefreshRequested>(_onTokenRefreshRequested);
    on<ProfileUpdated>(_onProfileUpdated);

    // Sync state changes to the ValueNotifier
    stream.listen((state) {
      authNotifier.value = state;
    });
  }

  // ==========================================================================
  // EVENT HANDLERS
  // ==========================================================================

  Future<void> _onCheckAuthStatus(
    CheckAuthStatus event,
    Emitter<AuthState> emit,
  ) async {
    emit(const AuthLoading());

    try {
      // Check if we have stored tokens
      final hasTokens = await _authRepository.hasStoredTokens();
      if (!hasTokens) {
        emit(const AuthUnauthenticated());
        return;
      }

      // Validate access token
      final isValid = await _authRepository.validateAccessToken();
      if (!isValid) {
        // Try to refresh
        final refreshed = await _tryRefreshToken();
        if (!refreshed) {
          await _authRepository.clearStoredTokens();
          emit(const AuthUnauthenticated());
          return;
        }
      }

      // Get user profile
      final user = await _authRepository.getCurrentUser();
      if (user == null) {
        emit(const AuthUnauthenticated());
        return;
      }

      // Check if onboarding is complete
      final onboardingComplete = await _authRepository.isOnboardingComplete();

      // Get token expiration
      final expiresAt = await _authRepository.getTokenExpiration();

      emit(AuthAuthenticated(
        user: user,
        onboardingComplete: onboardingComplete,
        tokenExpiresAt: expiresAt ?? DateTime.now().add(const Duration(hours: 1)),
      ));
    } catch (e) {
      debugPrint('Auth check failed: $e');
      emit(const AuthUnauthenticated());
    }
  }

  Future<void> _onLoginRequested(
    LoginRequested event,
    Emitter<AuthState> emit,
  ) async {
    emit(const AuthLoading());

    try {
      final result = await _loginUseCase.execute(
        email: event.email,
        password: event.password,
      );

      if (result.isFailure) {
        emit(AuthError(
          message: result.error ?? 'Login failed',
          errorCode: result.errorCode,
        ));
        return;
      }

      final user = result.user!;
      final onboardingComplete = await _authRepository.isOnboardingComplete();
      final expiresAt = await _authRepository.getTokenExpiration();

      emit(AuthAuthenticated(
        user: user,
        onboardingComplete: onboardingComplete,
        tokenExpiresAt: expiresAt ?? DateTime.now().add(const Duration(hours: 1)),
      ));
    } catch (e) {
      emit(AuthError(
        message: e.toString(),
        errorCode: 'LOGIN_FAILED',
      ));
    }
  }

  Future<void> _onRegisterRequested(
    RegisterRequested event,
    Emitter<AuthState> emit,
  ) async {
    emit(const AuthLoading());

    try {
      final result = await _registerUseCase.execute(
        email: event.email,
        password: event.password,
        fullName: event.fullName,
        phone: event.phone,
      );

      if (result.isFailure) {
        emit(AuthError(
          message: result.error ?? 'Registration failed',
          errorCode: result.errorCode,
        ));
        return;
      }

      final user = result.user!;

      // New users must complete onboarding (legal consent)
      emit(AuthAuthenticated(
        user: user,
        onboardingComplete: false,
        tokenExpiresAt: DateTime.now().add(const Duration(hours: 1)),
      ));
    } catch (e) {
      emit(AuthError(
        message: e.toString(),
        errorCode: 'REGISTER_FAILED',
      ));
    }
  }

  Future<void> _onLogoutRequested(
    LogoutRequested event,
    Emitter<AuthState> emit,
  ) async {
    try {
      await _authRepository.logout();
    } catch (e) {
      debugPrint('Logout error: $e');
    } finally {
      emit(const AuthUnauthenticated());
    }
  }

  Future<void> _onOnboardingCompleted(
    OnboardingCompleted event,
    Emitter<AuthState> emit,
  ) async {
    final currentState = state;
    if (currentState is! AuthAuthenticated) {
      return;
    }

    try {
      await _authRepository.markOnboardingComplete();
      emit(currentState.copyWith(onboardingComplete: true));
    } catch (e) {
      debugPrint('Failed to mark onboarding complete: $e');
    }
  }

  Future<void> _onTokenRefreshRequested(
    TokenRefreshRequested event,
    Emitter<AuthState> emit,
  ) async {
    final currentState = state;
    if (currentState is! AuthAuthenticated) {
      return;
    }

    final refreshed = await _tryRefreshToken();
    if (!refreshed) {
      // Refresh failed, force logout
      add(const LogoutRequested());
      return;
    }

    final expiresAt = await _authRepository.getTokenExpiration();
    emit(currentState.copyWith(
      tokenExpiresAt: expiresAt ?? DateTime.now().add(const Duration(hours: 1)),
    ));
  }

  Future<void> _onProfileUpdated(
    ProfileUpdated event,
    Emitter<AuthState> emit,
  ) async {
    final currentState = state;
    if (currentState is! AuthAuthenticated) {
      return;
    }

    emit(currentState.copyWith(user: event.user));
  }

  // ==========================================================================
  // HELPERS
  // ==========================================================================

  Future<bool> _tryRefreshToken() async {
    try {
      final success = await _authRepository.refreshAccessToken();
      return success;
    } catch (e) {
      debugPrint('Token refresh failed: $e');
      return false;
    }
  }

  // ==========================================================================
  // PUBLIC API
  // ==========================================================================

  /// Check if user is currently authenticated
  bool get isAuthenticated => state is AuthAuthenticated;

  /// Get the current user (if authenticated)
  User? get currentUser {
    final s = state;
    return s is AuthAuthenticated ? s.user : null;
  }

  /// Get the current access token (for WebSocket auth, etc.)
  Future<String?> getAccessToken() => _authRepository.getAccessToken();

  /// Check if token needs refresh (within 5 minutes of expiry)
  bool get needsTokenRefresh {
    final s = state;
    if (s is! AuthAuthenticated) return false;
    final timeUntilExpiry = s.tokenExpiresAt.difference(DateTime.now());
    return timeUntilExpiry.inMinutes < 5;
  }

  // ==========================================================================
  // CLEANUP
  // ==========================================================================

  @override
  Future<void> close() {
    authNotifier.dispose();
    return super.close();
  }
}

// ============================================================================
// RESULT TYPE (for use cases)
// ============================================================================

/// Represents the result of an auth operation
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