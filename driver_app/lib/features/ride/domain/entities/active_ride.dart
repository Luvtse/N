import 'package:equatable/equatable.dart';

class ActiveRide extends Equatable {
  final String id;
  final String riderId;
  final String riderName;
  final String riderPhone;
  final double riderRating;
  final double pickupLat;
  final double pickupLng;
  final String pickupAddress;
  final double dropoffLat;
  final double dropoffLng;
  final String dropoffAddress;
  final double distanceKm;
  final int estimatedDurationMinutes;
  final double fareAmount;
  final double tipAmount;
  final String status;
  final DateTime requestedAt;
  final DateTime? acceptedAt;
  final DateTime? startedAt;
  final DateTime? completedAt;
  final String? riderNote;

  const ActiveRide({
    required this.id,
    required this.riderId,
    required this.riderName,
    required this.riderPhone,
    required this.riderRating,
    required this.pickupLat,
    required this.pickupLng,
    required this.pickupAddress,
    required this.dropoffLat,
    required this.dropoffLng,
    required this.dropoffAddress,
    required this.distanceKm,
    required this.estimatedDurationMinutes,
    required this.fareAmount,
    required this.tipAmount,
    required this.status,
    required this.requestedAt,
    this.acceptedAt,
    this.startedAt,
    this.completedAt,
    this.riderNote,
  });

  factory ActiveRide.fromJson(Map<String, dynamic> json) {
    return ActiveRide(
      id: json['id'] as String,
      riderId: json['rider_id'] as String,
      riderName: json['rider_name'] as String,
      riderPhone: json['rider_phone'] as String,
      riderRating: (json['rider_rating'] as num).toDouble(),
      pickupLat: (json['pickup_lat'] as num).toDouble(),
      pickupLng: (json['pickup_lng'] as num).toDouble(),
      pickupAddress: json['pickup_address'] as String,
      dropoffLat: (json['dropoff_lat'] as num).toDouble(),
      dropoffLng: (json['dropoff_lng'] as num).toDouble(),
      dropoffAddress: json['dropoff_address'] as String,
      distanceKm: (json['distance_km'] as num).toDouble(),
      estimatedDurationMinutes: json['estimated_duration_minutes'] as int,
      fareAmount: (json['fare_amount'] as num).toDouble(),
      tipAmount: (json['tip_amount'] as num).toDouble(),
      status: json['status'] as String,
      requestedAt: DateTime.parse(json['requested_at'] as String),
      acceptedAt: json['accepted_at'] != null
          ? DateTime.parse(json['accepted_at'] as String)
          : null,
      startedAt: json['started_at'] != null
          ? DateTime.parse(json['started_at'] as String)
          : null,
      completedAt: json['completed_at'] != null
          ? DateTime.parse(json['completed_at'] as String)
          : null,
      riderNote: json['rider_note'] as String?,
    );
  }

  Map<String, dynamic> toJson() {
    return {
      'id': id,
      'rider_id': riderId,
      'rider_name': riderName,
      'rider_phone': riderPhone,
      'rider_rating': riderRating,
      'pickup_lat': pickupLat,
      'pickup_lng': pickupLng,
      'pickup_address': pickupAddress,
      'dropoff_lat': dropoffLat,
      'dropoff_lng': dropoffLng,
      'dropoff_address': dropoffAddress,
      'distance_km': distanceKm,
      'estimated_duration_minutes': estimatedDurationMinutes,
      'fare_amount': fareAmount,
      'tip_amount': tipAmount,
      'status': status,
      'requested_at': requestedAt.toIso8601String(),
      'accepted_at': acceptedAt?.toIso8601String(),
      'started_at': startedAt?.toIso8601String(),
      'completed_at': completedAt?.toIso8601String(),
      'rider_note': riderNote,
    };
  }

  ActiveRide copyWith({
    String? status,
    DateTime? acceptedAt,
    DateTime? startedAt,
    DateTime? completedAt,
  }) {
    return ActiveRide(
      id: id,
      riderId: riderId,
      riderName: riderName,
      riderPhone: riderPhone,
      riderRating: riderRating,
      pickupLat: pickupLat,
      pickupLng: pickupLng,
      pickupAddress: pickupAddress,
      dropoffLat: dropoffLat,
      dropoffLng: dropoffLng,
      dropoffAddress: dropoffAddress,
      distanceKm: distanceKm,
      estimatedDurationMinutes: estimatedDurationMinutes,
      fareAmount: fareAmount,
      tipAmount: tipAmount,
      status: status ?? this.status,
      requestedAt: requestedAt,
      acceptedAt: acceptedAt ?? this.acceptedAt,
      startedAt: startedAt ?? this.startedAt,
      completedAt: completedAt ?? this.completedAt,
      riderNote: riderNote,
    );
  }

  bool get isPending => status == 'pending';
  bool get isAccepted => status == 'accepted';
  bool get isInProgress => status == 'in_progress';
  bool get isCompleted => status == 'completed';

  double get totalEarnings => fareAmount + tipAmount;

  @override
  List<Object?> get props => [
        id,
        riderId,
        status,
        pickupLat,
        pickupLng,
        dropoffLat,
        dropoffLng,
        fareAmount,
      ];
}