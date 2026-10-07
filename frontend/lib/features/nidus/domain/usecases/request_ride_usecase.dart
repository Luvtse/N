import '../../data/repositories/ride_repository.dart';
import '../../data/repositories/ride_repository.dart';

/// Use case for requesting a new ride
class RequestRideUseCase {
  final RideRepository _rideRepository;

  RequestRideUseCase(this._rideRepository);

  /// Execute the ride request
  ///
  /// Parameters:
  /// - [pickupLat]: Pickup latitude
  /// - [pickupLng]: Pickup longitude
  /// - [dropoffLat]: Dropoff latitude
  /// - [dropoffLng]: Dropoff longitude
  /// - [pickupAddress]: Optional pickup address
  /// - [dropoffAddress]: Optional dropoff address
  /// - [rideType]: Type of ride (standard, premium, electric, etc.)
  /// - [paymentMethodId]: Payment method identifier
  ///
  /// Returns:
  /// - [Ride]: Created ride entity
  ///
  /// Throws:
  /// - [Exception]: If validation fails or request fails
  Future<Ride> execute({
    required double pickupLat,
    required double pickupLng,
    required double dropoffLat,
    required double dropoffLng,
    String? pickupAddress,
    String? dropoffAddress,
    String rideType = 'standard',
    String? paymentMethodId,
  }) async {
    // Validate inputs
    _validateInputs(
      pickupLat: pickupLat,
      pickupLng: pickupLng,
      dropoffLat: dropoffLat,
      dropoffLng: dropoffLng,
      rideType: rideType,
    );

    // Check if pickup and dropoff are different
    if (pickupLat == dropoffLat && pickupLng == dropoffLng) {
      throw Exception('Pickup and dropoff locations must be different');
    }

    // Calculate distance (optional pre-check)
    final distance = _calculateDistance(
      pickupLat,
      pickupLng,
      dropoffLat,
      dropoffLng,
    );

    if (distance < 0.1) {
      throw Exception('Distance is too short');
    }

    if (distance > 500) {
      throw Exception('Distance is too long (max 500 km)');
    }

    // Request ride from repository
    try {
      final ride = await _rideRepository.requestRide(
        pickupLat: pickupLat,
        pickupLng: pickupLng,
        dropoffLat: dropoffLat,
        dropoffLng: dropoffLng,
        pickupAddress: pickupAddress,
        dropoffAddress: dropoffAddress,
        rideType: rideType,
        paymentMethodId: paymentMethodId,
      );

      return ride;
    } catch (e) {
      throw Exception('Failed to request ride: ${e.toString()}');
    }
  }

  /// Validate input parameters
  void _validateInputs({
    required double pickupLat,
    required double pickupLng,
    required double dropoffLat,
    required double dropoffLng,
    required String rideType,
  }) {
    // Validate latitude
    if (pickupLat < -90 || pickupLat > 90) {
      throw Exception('Invalid pickup latitude');
    }
    if (dropoffLat < -90 || dropoffLat > 90) {
      throw Exception('Invalid dropoff latitude');
    }

    // Validate longitude
    if (pickupLng < -180 || pickupLng > 180) {
      throw Exception('Invalid pickup longitude');
    }
    if (dropoffLng < -180 || dropoffLng > 180) {
      throw Exception('Invalid dropoff longitude');
    }

    // Validate ride type
    const validRideTypes = [
      'standard',
      'premium',
      'electric',
      'shared',
      'wheelchair',
    ];

    if (!validRideTypes.contains(rideType)) {
      throw Exception('Invalid ride type: $rideType');
    }
  }

  /// Calculate distance between two points (Haversine formula)
  double _calculateDistance(
    double lat1,
    double lng1,
    double lat2,
    double lng2,
  ) {
    const earthRadiusKm = 6371.0;

    final dLat = _degreesToRadians(lat2 - lat1);
    final dLng = _degreesToRadians(lng2 - lng1);

    final a = math.sin(dLat / 2) * math.sin(dLat / 2) +
        math.cos(_degreesToRadians(lat1)) *
            math.cos(_degreesToRadians(lat2)) *
            math.sin(dLng / 2) *
            math.sin(dLng / 2);

    final c = 2 * math.atan2(math.sqrt(a), math.sqrt(1 - a));

    return earthRadiusKm * c;
  }

  /// Convert degrees to radians
  double _degreesToRadians(double degrees) {
    return degrees * math.pi / 180;
  }
}

// Import dart:math for math functions
import 'dart:math' as math;