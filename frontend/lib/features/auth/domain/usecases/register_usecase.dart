import 'package:equatable/equatable.dart';

import '../../data/repositories/auth_repository.dart';

// ============================================================================
// INPUT PARAMETERS
// ============================================================================

/// Parameters for the register use case with validation rules
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

  /// Validate all registration fields
  /// Returns a list of error messages (empty if valid)
  List<String> validate() {
    final errors = <String>[];

    // Email validation
    if (email.trim().isEmpty) {
      errors.add('Email is required');
    } else if (!_isValidEmail(email)) {
      errors.add('Invalid email format');
    }

    // Password validation
    if (password.isEmpty) {
      errors.add('Password is required');
    } else {
      if (password.length < 8) {
        errors.add('Password must be at least 8 characters');
      }
      if (!_hasUppercase(password)) {
        errors.add('Password must contain an uppercase letter');
      }
      if (!_hasLowercase(password)) {
        errors.add('Password must contain a lowercase letter');
      }
      if (!_hasNumber(password)) {
        errors.add('Password must contain a number');
      }
    }

    // Full name validation
    if (fullName.trim().isEmpty) {
      errors.add('Full name is required');
    } else if (fullName.trim().length < 2) {
      errors.add('Full name must be at least 2 characters');
    } else if (fullName.trim().length > 100) {
      errors.add('Full name must be less than 100 characters');
    }

    // Phone validation
    if (phone.trim().isEmpty) {
      errors.add('Phone number is required');
    } else if (!_isValidPhone(phone)) {
      errors.add('Invalid phone number format');
    }

    return errors;
  }

  bool _isValidEmail(String email) {
    final regex = RegExp(r'^[\w-\.]+@([\w-]+\.)+[\w-]{2,4}$');
    return regex.hasMatch(email.trim());
  }

  bool _hasUppercase(String s) => s.contains(RegExp(r'[A-Z]'));
  bool _hasLowercase(String s) => s.contains(RegExp(r'[a-z]'));
  bool _hasNumber(String s) => s.contains(RegExp(r'[0-9]'));

  bool _isValidPhone(String phone) {
    // Allows optional + prefix, digits, spaces, dashes, parentheses
    final regex = RegExp(r'^\+?[\d\s\-\(\)]{7,20}$');
    return regex.hasMatch(phone.trim());
  }
}

// ============================================================================
// USE CASE
// ============================================================================

/// Use case for user registration
///
/// Encapsulates business logic for creating a new user account:
/// 1. Validates input against security rules
/// 2. Normalizes data (email lowercase, phone format)
/// 3. Delegates to repository for persistence
/// 4. Returns a typed result
class RegisterUseCase {
  final AuthRepository _authRepository;

  const RegisterUseCase(this._authRepository);

  /// Execute the registration use case
  ///
  /// Returns [AuthResult.success] with the new user on success,
  /// or [AuthResult.failure] with error details on failure.
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
        error: validationErrors.first,
        errorCode: 'VALIDATION_ERROR',
      );
    }

    // 2. Normalize input
    final normalizedEmail = email.trim().toLowerCase();
    final normalizedPhone = _normalizePhone(phone);
    final normalizedName = _normalizeFullName(fullName);

    // 3. Delegate to repository
    try {
      return await _authRepository.register(
        email: normalizedEmail,
        password: password,
        fullName: normalizedName,
        phone: normalizedPhone,
      );
    } catch (e) {
      return AuthResult.failure(
        error: 'Registration failed: ${e.toString()}',
        errorCode: 'UNKNOWN_ERROR',
      );
    }
  }

  String _normalizePhone(String phone) {
    // Remove spaces, dashes, and parentheses; keep + prefix
    return phone.trim().replaceAll(RegExp(r'[\s\-\(\)]'), '');
  }

  String _normalizeFullName(String name) {
    // Trim whitespace and collapse multiple spaces
    return name.trim().replaceAll(RegExp(r'\s+'), ' ');
  }
}