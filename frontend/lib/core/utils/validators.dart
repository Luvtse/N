/// Input validation utilities for NIDAW
class Validators {
  Validators._();

  // ============================================================================
  // EMAIL VALIDATION
  // ============================================================================

  /// Validate email format
  static String? validateEmail(String? value) {
    if (value == null || value.trim().isEmpty) {
      return 'Email is required';
    }

    final emailRegex = RegExp(
      r'^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$',
    );

    if (!emailRegex.hasMatch(value.trim())) {
      return 'Please enter a valid email address';
    }

    if (value.length > 255) {
      return 'Email must be less than 255 characters';
    }

    return null;
  }

  // ============================================================================
  // PASSWORD VALIDATION
  // ============================================================================

  /// Validate password strength
  static String? validatePassword(String? value) {
    if (value == null || value.isEmpty) {
      return 'Password is required';
    }

    if (value.length < 8) {
      return 'Password must be at least 8 characters';
    }

    if (value.length > 128) {
      return 'Password must be less than 128 characters';
    }

    if (!value.contains(RegExp(r'[A-Z]'))) {
      return 'Password must contain at least one uppercase letter';
    }

    if (!value.contains(RegExp(r'[a-z]'))) {
      return 'Password must contain at least one lowercase letter';
    }

    if (!value.contains(RegExp(r'[0-9]'))) {
      return 'Password must contain at least one number';
    }

    return null;
  }

  /// Validate password confirmation
  static String? validatePasswordConfirmation(String? value, String password) {
    if (value == null || value.isEmpty) {
      return 'Please confirm your password';
    }

    if (value != password) {
      return 'Passwords do not match';
    }

    return null;
  }

  // ============================================================================
  // PHONE VALIDATION
  // ============================================================================

  /// Validate phone number format
  static String? validatePhone(String? value) {
    if (value == null || value.trim().isEmpty) {
      return 'Phone number is required';
    }

    // Remove spaces, dashes, and parentheses for validation
    final cleaned = value.replaceAll(RegExp(r'[\s\-\(\)]'), '');

    // Check if it starts with + and contains only digits after
    final phoneRegex = RegExp(r'^\+?[0-9]{7,15}$');

    if (!phoneRegex.hasMatch(cleaned)) {
      return 'Please enter a valid phone number';
    }

    return null;
  }

  // ============================================================================
  // NAME VALIDATION
  // ============================================================================

  /// Validate full name
  static String? validateFullName(String? value) {
    if (value == null || value.trim().isEmpty) {
      return 'Full name is required';
    }

    if (value.trim().length < 2) {
      return 'Name must be at least 2 characters';
    }

    if (value.trim().length > 100) {
      return 'Name must be less than 100 characters';
    }

    // Check for valid characters (letters, spaces, hyphens, apostrophes)
    final nameRegex = RegExp(r"^[a-zA-Z\s\-']+$");

    if (!nameRegex.hasMatch(value.trim())) {
      return 'Name contains invalid characters';
    }

    return null;
  }

  // ============================================================================
  // LOCATION VALIDATION
  // ============================================================================

  /// Validate latitude
  static String? validateLatitude(double? value) {
    if (value == null) {
      return 'Latitude is required';
    }

    if (value < -90 || value > 90) {
      return 'Latitude must be between -90 and 90';
    }

    return null;
  }

  /// Validate longitude
  static String? validateLongitude(double? value) {
    if (value == null) {
      return 'Longitude is required';
    }

    if (value < -180 || value > 180) {
      return 'Longitude must be between -180 and 180';
    }

    return null;
  }

  // ============================================================================
  // AMOUNT VALIDATION
  // ============================================================================

  /// Validate monetary amount
  static String? validateAmount(String? value) {
    if (value == null || value.trim().isEmpty) {
      return 'Amount is required';
    }

    final amount = double.tryParse(value.trim());

    if (amount == null) {
      return 'Please enter a valid amount';
    }

    if (amount < 0) {
      return 'Amount cannot be negative';
    }

    if (amount > 999999.99) {
      return 'Amount is too large';
    }

    return null;
  }

  /// Validate tip amount
  static String? validateTip(String? value) {
    if (value == null || value.trim().isEmpty) {
      return null; // Tip is optional
    }

    final tip = double.tryParse(value.trim());

    if (tip == null) {
      return 'Please enter a valid tip amount';
    }

    if (tip < 0) {
      return 'Tip cannot be negative';
    }

    if (tip > 1000) {
      return 'Tip amount is too large';
    }

    return null;
  }

  // ============================================================================
  // URL VALIDATION
  // ============================================================================

  /// Validate URL format
  static String? validateUrl(String? value) {
    if (value == null || value.trim().isEmpty) {
      return 'URL is required';
    }

    final urlRegex = RegExp(
      r'^(https?:\/\/)?([\da-z\.-]+)\.([a-z\.]{2,6})([\/\w \.-]*)*\/?$',
    );

    if (!urlRegex.hasMatch(value.trim())) {
      return 'Please enter a valid URL';
    }

    return null;
  }

  // ============================================================================
  // REQUIRED FIELD VALIDATION
  // ============================================================================

  /// Validate required field
  static String? validateRequired(String? value, [String fieldName = 'Field']) {
    if (value == null || value.trim().isEmpty) {
      return '$fieldName is required';
    }

    return null;
  }

  // ============================================================================
  // VEHICLE VALIDATION
  // ============================================================================

  /// Validate license plate
  static String? validateLicensePlate(String? value) {
    if (value == null || value.trim().isEmpty) {
      return 'License plate is required';
    }

    if (value.trim().length < 2) {
      return 'License plate must be at least 2 characters';
    }

    if (value.trim().length > 10) {
      return 'License plate must be less than 10 characters';
    }

    return null;
  }

  /// Validate vehicle year
  static String? validateVehicleYear(String? value) {
    if (value == null || value.trim().isEmpty) {
      return 'Vehicle year is required';
    }

    final year = int.tryParse(value.trim());

    if (year == null) {
      return 'Please enter a valid year';
    }

    final currentYear = DateTime.now().year;

    if (year < 1900 || year > currentYear + 1) {
      return 'Please enter a valid year between 1900 and ${currentYear + 1}';
    }

    return null;
  }

  // ============================================================================
  // COMPOSITE VALIDATORS
  // ============================================================================

  /// Validate login form
  static Map<String, String?> validateLoginForm({
    required String email,
    required String password,
  }) {
    return {
      'email': validateEmail(email),
      'password': validatePassword(password),
    };
  }

  /// Validate registration form
  static Map<String, String?> validateRegistrationForm({
    required String email,
    required String password,
    required String fullName,
    required String phone,
  }) {
    return {
      'email': validateEmail(email),
      'password': validatePassword(password),
      'fullName': validateFullName(fullName),
      'phone': validatePhone(phone),
    };
  }

  /// Check if form has errors
  static bool hasErrors(Map<String, String?> errors) {
    return errors.values.any((error) => error != null);
  }

  /// Get first error message
  static String? getFirstError(Map<String, String?> errors) {
    for (final error in errors.values) {
      if (error != null) {
        return error;
      }
    }
    return null;
  }
}