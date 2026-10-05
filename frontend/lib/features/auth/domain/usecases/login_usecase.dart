import 'package:equatable/equatable.dart';

import '../../data/repositories/auth_repository.dart';

// ============================================================================
// INPUT PARAMETERS
// ============================================================================

/// Parameters for the login use case
class LoginParams extends Equatable {
  final String email;
  final String password;

  const LoginParams({
    required this.email,
    required this.password,
  });

  @override
  List<Object?> get props => [email, password];

  /// Validate the input parameters
  List<String> validate() {
    final errors = <String>[];

    if (email.isEmpty) {
      errors.add('Email is required');
    } else if (!_isValidEmail(email)) {
      errors.add('Invalid email format');
    }

    if (password.isEmpty) {
      errors.add('Password is required');
    } else if (password.length < 8) {
      errors.add('Password must be at least 8 characters');
    }

    return errors;
  }

  bool _isValidEmail(String email) {
    final emailRegex = RegExp(r'^[\w-\.]+@([\w-]+\.)+[\w-]{2,4}$');
    return emailRegex.hasMatch(email);
  }
}

// ============================================================================
// USE CASE
// ============================================================================

/// Use case for user login
/// 
/// This use case encapsulates the business logic for authenticating a user.
/// It validates input, calls the repository, and returns a typed result.
class LoginUseCase {
  final AuthRepository _authRepository;

  const LoginUseCase(this._authRepository);

  /// Execute the login use case
  /// 
  /// Returns an [AuthResult] containing either:
  /// - A [User] on success
  /// - An error message and code on failure
  Future<AuthResult> execute({
    required String email,
    required String password,
  }) async {
    // 1. Validate input
    final params = LoginParams(email: email, password: password);
    final validationErrors = params.validate();
    
    if (validationErrors.isNotEmpty) {
      return AuthResult.failure(
        error: validationErrors.join(', '),
        errorCode: 'VALIDATION_ERROR',
      );
    }

    // 2. Normalize input
    final normalizedEmail = email.trim().toLowerCase();

    // 3. Call repository
    final result = await _authRepository.login(
      email: normalizedEmail,
      password: password,
    );

    // 4. Return result (already typed by repository)
    return result;
  }

  /// Convenience method to check if login is possible without credentials
  Future<bool> hasActiveSession() async {
    final hasTokens = await _authRepository.hasStoredTokens();
    if (!hasTokens) return false;

    final isValid = await _authRepository.validateAccessToken();
    return isValid;
  }
}

// ============================================================================
// REGISTER USE CASE (for completeness)
// ============================================================================

/// Parameters for the register use case
class RegisterParams extends Equatable {
  final String email;
  final String password;
  final String fullName;
  final String phone;

  const RegisterParams({
    required this.email,
    required this.password,
    required this.fullName,
    required this.phone,
  });

  @override
  List<Object?> get props => [email, password, fullName, phone];

  /// Validate the input parameters
  List<String> validate() {
    final errors = <String>[];

    if (email.isEmpty) {
      errors.add('Email is required');
    } else if (!_isValidEmail(email)) {
      errors.add('Invalid email format');
    }

    if (password.isEmpty) {
      errors.add('Password is required');
    } else {
      if (password.length < 8) {
        errors.add('Password must be at least 8 characters');
      }
      if (!_hasUppercase(password)) {
        errors.add('Password must contain an uppercase letter');
      }
      if (!_hasNumber(password)) {
        errors.add('Password must contain a number');
      }
    }

    if (fullName.isEmpty) {
      errors.add('Full name is required');
    } else if (fullName.length < 2) {
      errors.add('Full name must be at least 2 characters');
    }

    if (phone.isEmpty) {
      errors.add('Phone number is required');
    } else if (!_isValidPhone(phone)) {
      errors.add('Invalid phone number format');
    }

    return errors;
  }

  bool _isValidEmail(String email) {
    final emailRegex = RegExp(r'^[\w-\.]+@([\w-]+\.)+[\w-]{2,4}$');
    return emailRegex.hasMatch(email);
  }

  bool _hasUppercase(String password) {
    return password.contains(RegExp(r'[A-Z]'));
  }

  bool _hasNumber(String password) {
    return password.contains(RegExp(r'[0-9]'));
  }

  bool _isValidPhone(String phone) {
    // Basic phone validation - allows +, digits, spaces, dashes
    final phoneRegex = RegExp(r'^\+?[\d\s-]{7,20}$');
    return phoneRegex.hasMatch(phone);
  }
}

/// Use case for user registration
class RegisterUseCase {
  final AuthRepository _authRepository;

  const RegisterUseCase(this._authRepository);

  /// Execute the register use case
  Future<AuthResult> execute({
    required String email,
    required String password,
    required String fullName,
    required String phone,
  }) async {
    // 1. Validate input
    final params = RegisterParams(
      email: email,
      password: password,
      fullName: fullName,
      phone: phone,
    );
    
    final validationErrors = params.validate();
    if (validationErrors.isNotEmpty) {
      return AuthResult.failure(
        error: validationErrors.join(', '),
        errorCode: 'VALIDATION_ERROR',
      );
    }

    // 2. Normalize input
    final normalizedEmail = email.trim().toLowerCase();
    final normalizedPhone = _normalizePhone(phone);
    final normalizedFullName = _normalizeFullName(fullName);

    // 3. Call repository
    return await _authRepository.register(
      email: normalizedEmail,
      password: password,
      fullName: normalizedFullName,
      phone: normalizedPhone,
    );
  }

  String _normalizePhone(String phone) {
    // Remove spaces and dashes, keep + prefix
    return phone.replaceAll(RegExp(r'[\s-]'), '');
  }

  String _normalizeFullName(String name) {
    // Trim and collapse multiple spaces
    return name.trim().replaceAll(RegExp(r'\s+'), ' ');
  }
}