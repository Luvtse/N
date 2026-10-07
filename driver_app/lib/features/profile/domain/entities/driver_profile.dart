import 'package:equatable/equatable.dart';

/// Driver profile domain entity (Phase A/A10).
///
/// Backs features/profile bloc, repository and pages. Mirrors the subset of
/// the driver record returned by `GET /api/v1/drivers/profile`.
class DriverProfile extends Equatable {
  final String id;
  final String userId;
  final String fullName;
  final String email;
  final String phone;
  final String? profileImageUrl;
  final double rating;
  final double acceptanceRate;
  final double completionRate;
  final int totalRides;
  final double totalEarnings;
  final String status;
  final String vehicleType;
  final String vehicleModel;
  final String vehiclePlate;
  final String vehicleColor;
  final int vehicleYear;
  final bool licenseVerified;
  final bool backgroundCheck;
  final bool vehicleInspected;
  final DateTime createdAt;

  const DriverProfile({
    required this.id,
    required this.userId,
    required this.fullName,
    required this.email,
    required this.phone,
    this.profileImageUrl,
    required this.rating,
    required this.acceptanceRate,
    required this.completionRate,
    required this.totalRides,
    required this.totalEarnings,
    required this.status,
    required this.vehicleType,
    required this.vehicleModel,
    required this.vehiclePlate,
    required this.vehicleColor,
    required this.vehicleYear,
    required this.licenseVerified,
    required this.backgroundCheck,
    required this.vehicleInspected,
    required this.createdAt,
  });

  factory DriverProfile.fromJson(Map<String, dynamic> json) {
    return DriverProfile(
      id: json['id'] as String,
      userId: json['user_id'] as String? ?? '',
      fullName: json['full_name'] as String,
      email: json['email'] as String? ?? '',
      phone: json['phone'] as String? ?? '',
      profileImageUrl: json['profile_image_url'] as String?,
      rating: (json['rating'] as num?)?.toDouble() ?? 0,
      acceptanceRate: (json['acceptance_rate'] as num?)?.toDouble() ?? 0,
      completionRate: (json['completion_rate'] as num?)?.toDouble() ?? 0,
      totalRides: (json['total_rides'] as num?)?.toInt() ?? 0,
      totalEarnings: (json['total_earnings'] as num?)?.toDouble() ?? 0,
      status: json['status'] as String? ?? 'active',
      vehicleType: json['vehicle_type'] as String? ?? '',
      vehicleModel: json['vehicle_model'] as String? ?? '',
      vehiclePlate: json['vehicle_plate'] as String? ?? '',
      vehicleColor: json['vehicle_color'] as String? ?? '',
      vehicleYear: (json['vehicle_year'] as num?)?.toInt() ?? 0,
      licenseVerified: json['license_verified'] as bool? ?? false,
      backgroundCheck: json['background_check'] as bool? ?? false,
      vehicleInspected: json['vehicle_inspected'] as bool? ?? false,
      createdAt: DateTime.tryParse(json['created_at'] as String? ?? '') ??
          DateTime.now(),
    );
  }

  Map<String, dynamic> toJson() => {
        'id': id,
        'user_id': userId,
        'full_name': fullName,
        'email': email,
        'phone': phone,
        'profile_image_url': profileImageUrl,
        'rating': rating,
        'acceptance_rate': acceptanceRate,
        'completion_rate': completionRate,
        'total_rides': totalRides,
        'total_earnings': totalEarnings,
        'status': status,
        'vehicle_type': vehicleType,
        'vehicle_model': vehicleModel,
        'vehicle_plate': vehiclePlate,
        'vehicle_color': vehicleColor,
        'vehicle_year': vehicleYear,
        'license_verified': licenseVerified,
        'background_check': backgroundCheck,
        'vehicle_inspected': vehicleInspected,
        'created_at': createdAt.toIso8601String(),
      };

  DriverProfile copyWith({
    String? fullName,
    String? email,
    String? phone,
    String? profileImageUrl,
    String? vehicleType,
    String? vehicleModel,
    String? vehiclePlate,
    String? vehicleColor,
    int? vehicleYear,
    bool? licenseVerified,
    bool? backgroundCheck,
    bool? vehicleInspected,
  }) {
    return DriverProfile(
      id: id,
      userId: userId,
      fullName: fullName ?? this.fullName,
      email: email ?? this.email,
      phone: phone ?? this.phone,
      profileImageUrl: profileImageUrl ?? this.profileImageUrl,
      rating: rating,
      acceptanceRate: acceptanceRate,
      completionRate: completionRate,
      totalRides: totalRides,
      totalEarnings: totalEarnings,
      status: status,
      vehicleType: vehicleType ?? this.vehicleType,
      vehicleModel: vehicleModel ?? this.vehicleModel,
      vehiclePlate: vehiclePlate ?? this.vehiclePlate,
      vehicleColor: vehicleColor ?? this.vehicleColor,
      vehicleYear: vehicleYear ?? this.vehicleYear,
      licenseVerified: licenseVerified ?? this.licenseVerified,
      backgroundCheck: backgroundCheck ?? this.backgroundCheck,
      vehicleInspected: vehicleInspected ?? this.vehicleInspected,
      createdAt: createdAt,
    );
  }

  @override
  List<Object?> get props => [
        id,
        userId,
        fullName,
        email,
        phone,
        profileImageUrl,
        rating,
        acceptanceRate,
        completionRate,
        totalRides,
        totalEarnings,
        status,
        vehicleType,
        vehicleModel,
        vehiclePlate,
        vehicleColor,
        vehicleYear,
        licenseVerified,
        backgroundCheck,
        vehicleInspected,
        createdAt,
      ];
}
