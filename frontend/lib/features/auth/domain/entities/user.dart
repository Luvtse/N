import 'package:equatable/equatable.dart';

// ============================================================================
// USER ROLES
// ============================================================================

/// User roles in the NIDAW system
enum UserRole {
  rider,
  driver,
  admin,
  corporate;

  /// Parse role from API string
  static UserRole fromString(String? value) {
    switch (value?.toLowerCase()) {
      case 'driver':
        return UserRole.driver;
      case 'admin':
        return UserRole.admin;
      case 'corporate':
        return UserRole.corporate;
      case 'rider':
      default:
        return UserRole.rider;
    }
  }

  /// Convert role to API string
  String toApiString() => name;
}

// ============================================================================
// USER ENTITY
// ============================================================================

/// Represents a user in the NIDAW system
///
/// This is a pure domain entity with no framework dependencies.
/// It is immutable and uses value equality via Equatable.
class User extends Equatable {
  final String id;
  final String email;
  final String fullName;
  final String phone;
  final String? profileImageUrl;
  final UserRole role;
  final String? countryCode;
  final bool emailVerified;
  final DateTime createdAt;
  final DateTime? updatedAt;

  const User({
    required this.id,
    required this.email,
    required this.fullName,
    required this.phone,
    this.profileImageUrl,
    this.role = UserRole.rider,
    this.countryCode,
    this.emailVerified = false,
    required this.createdAt,
    this.updatedAt,
  });

  // ==========================================================================
  // FACTORY METHODS
  // ==========================================================================

  /// Create a User from JSON (API response)
  factory User.fromJson(Map<String, dynamic> json) {
    return User(
      id: json['id'] as String,
      email: json['email'] as String,
      fullName: json['full_name'] as String? ?? json['fullName'] as String? ?? '',
      phone: json['phone'] as String? ?? '',
      profileImageUrl: json['profile_image_url'] as String?,
      role: UserRole.fromString(json['role'] as String?),
      countryCode: json['country_code'] as String?,
      emailVerified: json['email_verified'] as bool? ?? false,
      createdAt: _parseDateTime(json['created_at']),
      updatedAt: json['updated_at'] != null
          ? _parseDateTime(json['updated_at'])
          : null,
    );
  }

  static DateTime _parseDateTime(dynamic value) {
    if (value is String) return DateTime.parse(value);
    if (value is DateTime) return value;
    if (value is int) {
      return DateTime.fromMillisecondsSinceEpoch(value * 1000);
    }
    return DateTime.now();
  }

  // ==========================================================================
  // SERIALIZATION
  // ==========================================================================

  /// Convert to JSON for API requests or local storage
  Map<String, dynamic> toJson() {
    return {
      'id': id,
      'email': email,
      'full_name': fullName,
      'phone': phone,
      'profile_image_url': profileImageUrl,
      'role': role.toApiString(),
      'country_code': countryCode,
      'email_verified': emailVerified,
      'created_at': createdAt.toIso8601String(),
      'updated_at': updatedAt?.toIso8601String(),
    };
  }

  // ==========================================================================
  // COPY WITH (for immutable updates)
  // ==========================================================================

  User copyWith({
    String? email,
    String? fullName,
    String? phone,
    String? profileImageUrl,
    UserRole? role,
    String? countryCode,
    bool? emailVerified,
    DateTime? updatedAt,
  }) {
    return User(
      id: id,
      email: email ?? this.email,
      fullName: fullName ?? this.fullName,
      phone: phone ?? this.phone,
      profileImageUrl: profileImageUrl ?? this.profileImageUrl,
      role: role ?? this.role,
      countryCode: countryCode ?? this.countryCode,
      emailVerified: emailVerified ?? this.emailVerified,
      createdAt: createdAt,
      updatedAt: updatedAt ?? this.updatedAt,
    );
  }

  // ==========================================================================
  // HELPERS
  // ==========================================================================

  /// Get user's initials for avatar fallback
  String get initials {
    final parts = fullName.trim().split(' ');
    if (parts.isEmpty) return '?';
    if (parts.length == 1) return parts[0][0].toUpperCase();
    return '${parts[0][0]}${parts[1][0]}'.toUpperCase();
  }

  /// Check if user is a driver
  bool get isDriver => role == UserRole.driver;

  /// Check if user is an admin
  bool get isAdmin => role == UserRole.admin;

  /// Check if user is a corporate account
  bool get isCorporate => role == UserRole.corporate;

  /// Get display name (full name or email fallback)
  String get displayName => fullName.isNotEmpty ? fullName : email;

  // ==========================================================================
  // EQUALITY
  // ==========================================================================

  @override
  List<Object?> get props => [
        id,
        email,
        fullName,
        phone,
        profileImageUrl,
        role,
        countryCode,
        emailVerified,
        createdAt,
        updatedAt,
      ];

  @override
  String toString() => 'User(id: $id, email: $email, role: ${role.name})';
}