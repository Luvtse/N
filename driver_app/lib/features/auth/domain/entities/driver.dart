import 'package:equatable/equatable.dart';

class Driver extends Equatable {
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

  const Driver({
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

  factory Driver.fromJson(Map<String, dynamic> json) {
    return Driver(
      id: json['id'] as String,
      userId: json['user_id'] as String,
      fullName: json['full_name'] as String,
      email: json['email'] as String,
      phone: json['phone'] as String,
      profileImageUrl: json['profile_image_url'] as String?,
      rating: (json['rating'] as num).toDouble(),
      acceptanceRate: (json['acceptance_rate'] as num).toDouble(),
      completionRate: (json['completion_rate'] as num).toDouble(),
      totalRides: json['total_rides'] as int,
      totalEarnings: (json['total_earnings'] as num).toDouble(),
      status: json['status'] as String,
      vehicleType: json['vehicle_type'] as String,
      vehicleModel: json['vehicle_model'] as String,
      vehiclePlate: json['vehicle_plate'] as String,
      vehicleColor: json['vehicle_color'] as String,
      vehicleYear: json['vehicle_year'] as int,
      licenseVerified: json['license_verified'] as bool,
      backgroundCheck: json['background_check'] as bool,
      vehicleInspected: json['vehicle_inspected'] as bool,
      createdAt: DateTime.parse(json['created_at'] as String),
    );
  }

  Map<String, dynamic> toJson() {
    return {
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
  }

  Driver copyWith({
    String? status,
    double? rating,
    double? acceptanceRate,
    double? completionRate,
    int? totalRides,
    double? totalEarnings,
  }) {
    return Driver(
      id: id,
      userId: userId,
      fullName: fullName,
      email: email,
      phone: phone,
      profileImageUrl: profileImageUrl,
      rating: rating ?? this.rating,
      acceptanceRate: acceptanceRate ?? this.acceptanceRate,
      completionRate: completionRate ?? this.completionRate,
      totalRides: totalRides ?? this.totalRides,
      totalEarnings: totalEarnings ?? this.totalEarnings,
      status: status ?? this.status,
      vehicleType: vehicleType,
      vehicleModel: vehicleModel,
      vehiclePlate: vehiclePlate,
      vehicleColor: vehicleColor,
      vehicleYear: vehicleYear,
      licenseVerified: licenseVerified,
      backgroundCheck: backgroundCheck,
      vehicleInspected: vehicleInspected,
      createdAt: createdAt,
    );
  }

  bool get isOnline => status == 'available';
  bool get isVerified => licenseVerified && backgroundCheck && vehicleInspected;

  @override
  List<Object?> get props => [
        id,
        userId,
        fullName,
        email,
        phone,
        status,
        rating,
        acceptanceRate,
        completionRate,
        totalRides,
        totalEarnings,
      ];
}