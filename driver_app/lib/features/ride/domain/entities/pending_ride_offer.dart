import 'package:equatable/equatable.dart';

/// One row of the backend driver offer feed
/// (GET /api/v1/nidus/drivers/pending-rides -> PendingRideOffer).
///
/// Field names mirror the server JSON contract exactly
/// (backend/internal/modules/nidus/interfaces/http/handlers/ride_handler.go,
/// type PendingRideOffer). `estimatedFare` is a nullable pointer on the
/// server (`fare_amount,omitempty`): absent means "not priced yet", never 0 —
/// the UI must render an honest "fare not provided" state instead of a
/// fabricated number (Global Rule 2).
class PendingRideOffer extends Equatable {
  final String rideId;
  final double pickupLat;
  final double pickupLng;
  final double dropoffLat;
  final double dropoffLng;
  final String pickupAddress;
  final String dropoffAddress;
  final String rideType;
  final double? estimatedFare;
  final String currency;
  final String status;
  final DateTime requestedAt;

  const PendingRideOffer({
    required this.rideId,
    required this.pickupLat,
    required this.pickupLng,
    required this.dropoffLat,
    required this.dropoffLng,
    required this.pickupAddress,
    required this.dropoffAddress,
    required this.rideType,
    required this.estimatedFare,
    required this.currency,
    required this.status,
    required this.requestedAt,
  });

  factory PendingRideOffer.fromJson(Map<String, dynamic> json) {
    return PendingRideOffer(
      rideId: json['ride_id'] as String,
      pickupLat: (json['pickup_lat'] as num).toDouble(),
      pickupLng: (json['pickup_lng'] as num).toDouble(),
      dropoffLat: (json['dropoff_lat'] as num).toDouble(),
      dropoffLng: (json['dropoff_lng'] as num).toDouble(),
      pickupAddress: (json['pickup_address'] as String?) ?? '',
      dropoffAddress: (json['dropoff_address'] as String?) ?? '',
      rideType: (json['ride_type'] as String?) ?? 'standard',
      estimatedFare: (json['estimated_fare'] as num?)?.toDouble(),
      currency: (json['currency'] as String?) ?? '',
      status: (json['status'] as String?) ?? '',
      requestedAt: json['requested_at'] != null
          ? DateTime.parse(json['requested_at'] as String)
          : DateTime.now(),
    );
  }

  Map<String, dynamic> toJson() => {
        'ride_id': rideId,
        'pickup_lat': pickupLat,
        'pickup_lng': pickupLng,
        'dropoff_lat': dropoffLat,
        'dropoff_lng': dropoffLng,
        'pickup_address': pickupAddress,
        'dropoff_address': dropoffAddress,
        'ride_type': rideType,
        if (estimatedFare != null) 'estimated_fare': estimatedFare,
        'currency': currency,
        'status': status,
        'requested_at': requestedAt.toIso8601String(),
      };

  @override
  List<Object?> get props => [
        rideId,
        pickupLat,
        pickupLng,
        dropoffLat,
        dropoffLng,
        pickupAddress,
        dropoffAddress,
        rideType,
        estimatedFare,
        currency,
        status,
        requestedAt,
      ];
}
