import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';

import '../../../../core/network/api_client.dart';
import '../../../../core/network/websocket_client.dart';

// ============================================================================
// ABSTRACT INTERFACE
// ============================================================================

/// Abstract repository interface for ride operations
abstract class RideRepository {
  /// Request a new ride
  Future<RideResult> requestRide({
    required double pickupLat,
    required double pickupLng,
    required double dropoffLat,
    required double dropoffLng,
    String? pickupAddress,
    String? dropoffAddress,
    String rideType = 'standard',
    String? paymentMethodId,
  });

  /// Get ride details by ID
  Future<Ride?> getRide(String rideId);

  /// List user's rides with pagination
  Future<RideListResult> listRides({
    String? status,
    int limit = 20,
    int offset = 0,
  });

  /// Cancel a ride
  Future<bool> cancelRide(String rideId, {String? reason});

  /// Rate a completed ride
  Future<bool> rateRide(String rideId, int rating, {String? review, double? tip});

  /// Get fare estimate
  Future<FareEstimate?> getFareEstimate({
    required double pickupLat,
    required double pickupLng,
    required double dropoffLat,
    required double dropoffLng,
  });

  /// Subscribe to ride status updates via WebSocket
  Stream<RideStatusUpdate> subscribeToRideUpdates(String rideId);

  /// Unsubscribe from ride updates
  void unsubscribeFromRideUpdates(String rideId);

  /// Get the rider's currently active (non-terminal) ride, if any
  Future<Ride?> getActiveRide();

  /// Cancel a ride with full result semantics (fee + error code).
  ///
  /// Preferred over [cancelRide] for flows that must surface cancellation
  /// fees or react to specific failure codes (e.g. INVALID_TRANSITION).
  Future<CancelRideResult> cancelRideWithResult(
    String rideId, {
    String? reason,
  });

  /// Rate a completed ride with full result semantics (error code).
  ///
  /// Preferred over [rateRide] for flows that must distinguish
  /// ALREADY_RATED / TIP_TOO_LARGE / TIP_SETTLEMENT_FAILED.
  Future<RateRideResult> rateRideWithResult(
    String rideId,
    int rating, {
    String? review,
    double? tip,
  });
}

// ============================================================================
// DOMAIN ENTITIES
// ============================================================================

/// Ride entity
class Ride {
  final String id;
  final String userId;
  final String? driverId;
  final double pickupLat;
  final double pickupLng;
  final double dropoffLat;
  final double dropoffLng;
  final String? pickupAddress;
  final String? dropoffAddress;
  final String rideType;
  final String status;
  final double? fareAmount;
  final String currency;
  final double? distanceKm;
  final int? durationMinutes;
  final DateTime requestedAt;
  final DateTime? matchedAt;
  final DateTime? completedAt;
  final DriverInfo? driver;

  const Ride({
    required this.id,
    required this.userId,
    this.driverId,
    required this.pickupLat,
    required this.pickupLng,
    required this.dropoffLat,
    required this.dropoffLng,
    this.pickupAddress,
    this.dropoffAddress,
    required this.rideType,
    required this.status,
    this.fareAmount,
    required this.currency,
    this.distanceKm,
    this.durationMinutes,
    required this.requestedAt,
    this.matchedAt,
    this.completedAt,
    this.driver,
  });

  factory Ride.fromJson(Map<String, dynamic> json) {
    return Ride(
      id: json['id'] as String,
      userId: json['user_id'] as String,
      driverId: json['driver_id'] as String?,
      pickupLat: (json['pickup_lat'] as num).toDouble(),
      pickupLng: (json['pickup_lng'] as num).toDouble(),
      dropoffLat: (json['dropoff_lat'] as num).toDouble(),
      dropoffLng: (json['dropoff_lng'] as num).toDouble(),
      pickupAddress: json['pickup_address'] as String?,
      dropoffAddress: json['dropoff_address'] as String?,
      rideType: json['ride_type'] as String? ?? 'standard',
      status: json['status'] as String,
      fareAmount: json['fare_amount'] != null
          ? (json['fare_amount'] as num).toDouble()
          : null,
      currency: json['currency'] as String? ?? 'USD',
      distanceKm: json['distance_km'] != null
          ? (json['distance_km'] as num).toDouble()
          : null,
      durationMinutes: json['duration_minutes'] as int?,
      requestedAt: DateTime.parse(json['requested_at'] as String),
      matchedAt: json['matched_at'] != null
          ? DateTime.parse(json['matched_at'] as String)
          : null,
      completedAt: json['completed_at'] != null
          ? DateTime.parse(json['completed_at'] as String)
          : null,
      driver: json['driver'] != null
          ? DriverInfo.fromJson(json['driver'] as Map<String, dynamic>)
          : null,
    );
  }
}

/// Driver information
class DriverInfo {
  final String id;
  final String name;
  final double rating;
  final String vehicleType;
  final String vehiclePlate;
  final double currentLat;
  final double currentLng;
  final int etaMinutes;

  const DriverInfo({
    required this.id,
    required this.name,
    required this.rating,
    required this.vehicleType,
    required this.vehiclePlate,
    required this.currentLat,
    required this.currentLng,
    required this.etaMinutes,
  });

  factory DriverInfo.fromJson(Map<String, dynamic> json) {
    return DriverInfo(
      id: json['id'] as String,
      name: json['name'] as String,
      rating: (json['rating'] as num).toDouble(),
      vehicleType: json['vehicle_type'] as String,
      vehiclePlate: json['vehicle_plate'] as String,
      currentLat: (json['current_lat'] as num).toDouble(),
      currentLng: (json['current_lng'] as num).toDouble(),
      etaMinutes: json['eta_minutes'] as int,
    );
  }
}

/// Fare estimate
class FareEstimate {
  final String rideType;
  final double baseFare;
  final double distanceFare;
  final double timeFare;
  final double subtotal;
  final double surgeMultiplier;
  final double totalFare;
  final String currency;
  final double distanceKm;
  final int durationMinutes;
  final int estimatedPickupMinutes;

  const FareEstimate({
    required this.rideType,
    required this.baseFare,
    required this.distanceFare,
    required this.timeFare,
    required this.subtotal,
    required this.surgeMultiplier,
    required this.totalFare,
    required this.currency,
    required this.distanceKm,
    required this.durationMinutes,
    required this.estimatedPickupMinutes,
  });

  factory FareEstimate.fromJson(Map<String, dynamic> json) {
    return FareEstimate(
      rideType: json['ride_type'] as String,
      baseFare: (json['base_fare'] as num).toDouble() / 100, // cents to dollars
      distanceFare: (json['distance_fare'] as num).toDouble() / 100,
      timeFare: (json['time_fare'] as num).toDouble() / 100,
      subtotal: (json['subtotal'] as num).toDouble() / 100,
      surgeMultiplier: (json['surge_multiplier'] as num).toDouble(),
      totalFare: (json['total_fare'] as num).toDouble() / 100,
      currency: json['currency'] as String? ?? 'USD',
      distanceKm: (json['distance_km'] as num).toDouble(),
      durationMinutes: json['duration_minutes'] as int,
      estimatedPickupMinutes: json['estimated_pickup_minutes'] as int,
    );
  }
}

/// Ride list result with pagination
class RideListResult {
  final List<Ride> rides;
  final int total;
  final int limit;
  final int offset;
  final bool hasMore;

  const RideListResult({
    required this.rides,
    required this.total,
    required this.limit,
    required this.offset,
    required this.hasMore,
  });

  factory RideListResult.fromJson(Map<String, dynamic> json) {
    return RideListResult(
      rides: (json['rides'] as List)
          .map((r) => Ride.fromJson(r as Map<String, dynamic>))
          .toList(),
      total: json['total'] as int,
      limit: json['limit'] as int,
      offset: json['offset'] as int,
      hasMore: json['has_more'] as bool,
    );
  }
}

/// Ride result from request
class RideResult {
  final String? rideId;
  final String? status;
  final double? fare;
  final int? eta;
  final String? error;
  final String? errorCode;

  const RideResult._({
    this.rideId,
    this.status,
    this.fare,
    this.eta,
    this.error,
    this.errorCode,
  });

  const RideResult.success({
    required this.rideId,
    required this.status,
    required this.fare,
    required this.eta,
  }) : error = null,
       errorCode = null;

  const RideResult.failure({
    required this.error,
    this.errorCode,
  }) : rideId = null,
       status = null,
       fare = null,
       eta = null;

  bool get isSuccess => rideId != null;
  bool get isFailure => error != null;
}

/// Ride status update from WebSocket
class RideStatusUpdate {
  final String rideId;
  final String status;
  final DriverInfo? driver;
  final double? driverLat;
  final double? driverLng;
  final DateTime timestamp;

  /// Who initiated a cancellation: 'rider', 'driver', or 'system'.
  /// Null for non-cancellation updates.
  final String? cancelledBy;

  /// Optional human-readable cancellation reason from the backend event.
  final String? cancelReason;

  const RideStatusUpdate({
    required this.rideId,
    required this.status,
    this.driver,
    this.driverLat,
    this.driverLng,
    required this.timestamp,
    this.cancelledBy,
    this.cancelReason,
  });

  factory RideStatusUpdate.fromJson(Map<String, dynamic> json) {
    return RideStatusUpdate(
      rideId: json['ride_id'] as String,
      status: json['status'] as String,
      driver: json['driver'] != null
          ? DriverInfo.fromJson(json['driver'] as Map<String, dynamic>)
          : null,
      driverLat: json['driver_lat'] != null
          ? (json['driver_lat'] as num).toDouble()
          : null,
      driverLng: json['driver_lng'] != null
          ? (json['driver_lng'] as num).toDouble()
          : null,
      timestamp: DateTime.parse(json['timestamp'] as String),
      cancelledBy: json['cancelled_by'] as String?,
      cancelReason: json['reason'] as String?,
    );
  }
}

// ============================================================================
// CANCEL / RATE RESULTS
// ============================================================================

/// Result of a cancellation attempt, including fee transparency data.
class CancelRideResult {
  final bool success;
  final String? errorCode;
  final String? message;

  /// Fee charged for cancelling, in currency units (0 when free window).
  final double cancellationFee;
  final String currency;

  const CancelRideResult._({
    required this.success,
    this.errorCode,
    this.message,
    this.cancellationFee = 0,
    this.currency = 'ETB',
  });

  const CancelRideResult.cancelled({
    this.cancellationFee = 0,
    this.currency = 'ETB',
  })  : success = true,
        errorCode = null,
        message = null;

  const CancelRideResult.failure({
    required this.errorCode,
    required this.message,
  })  : success = false,
        cancellationFee = 0,
        currency = 'ETB';

  /// True when the driver (not the rider) ended the ride — fee should be waived.
  bool get wasDriverCancelled => errorCode == 'DRIVER_CANCELLED';
}

/// Result of a rating attempt with coded failures surfaced by the backend.
class RateRideResult {
  final bool success;
  final String? errorCode;
  final String? message;

  const RateRideResult._({
    required this.success,
    this.errorCode,
    this.message,
  });

  const RateRideResult.rated()
      : success = true,
        errorCode = null,
        message = null;

  const RateRideResult.failure({
    required this.errorCode,
    required this.message,
  }) : success = false;

  bool get alreadyRated => errorCode == 'ALREADY_RATED';
  bool get tipTooLarge => errorCode == 'TIP_TOO_LARGE';
  bool get tipSettlementFailed => errorCode == 'TIP_SETTLEMENT_FAILED';
}

// ============================================================================
// IMPLEMENTATION
// ============================================================================

/// Concrete implementation of RideRepository
class RideRepositoryImpl implements RideRepository {
  final ApiClient _apiClient;
  final WebSocketClient _webSocketClient;

  // Track active subscriptions
  final Map<String, StreamController<RideStatusUpdate>> _subscriptions = {};

  RideRepositoryImpl({
    required ApiClient apiClient,
    required WebSocketClient webSocketClient,
  })  : _apiClient = apiClient,
        _webSocketClient = webSocketClient;

  // ==========================================================================
  // RIDE OPERATIONS
  // ==========================================================================

  @override
  Future<RideResult> requestRide({
    required double pickupLat,
    required double pickupLng,
    required double dropoffLat,
    required double dropoffLng,
    String? pickupAddress,
    String? dropoffAddress,
    String rideType = 'standard',
    String? paymentMethodId,
  }) async {
    try {
      final response = await _apiClient.post(
        '/api/v1/nidus/rides',
        data: {
          'pickup_lat': pickupLat,
          'pickup_lng': pickupLng,
          'dropoff_lat': dropoffLat,
          'dropoff_lng': dropoffLng,
          'pickup_address': pickupAddress,
          'dropoff_address': dropoffAddress,
          'ride_type': rideType,
          'payment_method_id': paymentMethodId,
        },
      );

      final data = response.data as Map<String, dynamic>;

      return RideResult.success(
        rideId: data['ride_id'] as String,
        status: data['status'] as String,
        fare: (data['fare'] as num).toDouble(),
        eta: data['eta'] as int,
      );
    } on ApiException catch (e) {
      return RideResult.failure(
        error: e.message,
        errorCode: e.errorCode,
      );
    } catch (e) {
      return RideResult.failure(
        error: 'Failed to request ride: ${e.toString()}',
        errorCode: 'UNKNOWN_ERROR',
      );
    }
  }

  @override
  Future<Ride?> getRide(String rideId) async {
    try {
      final response = await _apiClient.get('/api/v1/nidus/rides/$rideId');
      final data = response.data as Map<String, dynamic>;
      return Ride.fromJson(data);
    } catch (e) {
      debugPrint('Failed to get ride: $e');
      return null;
    }
  }

  @override
  Future<RideListResult> listRides({
    String? status,
    int limit = 20,
    int offset = 0,
  }) async {
    try {
      final queryParams = <String, dynamic>{
        'limit': limit,
        'offset': offset,
      };
      if (status != null) {
        queryParams['status'] = status;
      }

      final response = await _apiClient.get(
        '/api/v1/nidus/rides',
        queryParameters: queryParams,
      );

      final data = response.data as Map<String, dynamic>;
      return RideListResult.fromJson(data);
    } catch (e) {
      debugPrint('Failed to list rides: $e');
      return RideListResult(
        rides: [],
        total: 0,
        limit: limit,
        offset: offset,
        hasMore: false,
      );
    }
  }

  @override
  Future<bool> cancelRide(String rideId, {String? reason}) async {
    try {
      await _apiClient.post(
        '/api/v1/nidus/rides/$rideId/cancel',
        data: {'reason': reason},
      );
      return true;
    } catch (e) {
      debugPrint('Failed to cancel ride: $e');
      return false;
    }
  }

  @override
  Future<bool> rateRide(
    String rideId,
    int rating, {
    String? review,
    double? tip,
  }) async {
    try {
      await _apiClient.post(
        '/api/v1/nidus/rides/$rideId/rate',
        data: {
          'rating': rating,
          'review': review,
          'tip': tip,
        },
      );
      return true;
    } catch (e) {
      debugPrint('Failed to rate ride: $e');
      return false;
    }
  }

  @override
  Future<FareEstimate?> getFareEstimate({
    required double pickupLat,
    required double pickupLng,
    required double dropoffLat,
    required double dropoffLng,
  }) async {
    try {
      final response = await _apiClient.post(
        '/api/v1/nidus/rides/estimate',
        data: {
          'pickup_lat': pickupLat,
          'pickup_lng': pickupLng,
          'dropoff_lat': dropoffLat,
          'dropoff_lng': dropoffLng,
        },
      );

      final data = response.data as Map<String, dynamic>;
      return FareEstimate.fromJson(data);
    } catch (e) {
      debugPrint('Failed to get fare estimate: $e');
      return null;
    }
  }

  // ==========================================================================
  // WEBSOCKET SUBSCRIPTIONS
  // ==========================================================================

  @override
  Stream<RideStatusUpdate> subscribeToRideUpdates(String rideId) {
    if (_subscriptions.containsKey(rideId)) {
      return _subscriptions[rideId]!.stream;
    }

    final controller = StreamController<RideStatusUpdate>.broadcast();
    _subscriptions[rideId] = controller;

    // Subscribe to WebSocket
    _webSocketClient.subscribe('ride:$rideId', (message) {
      try {
        final data = jsonDecode(message) as Map<String, dynamic>;
        final update = RideStatusUpdate.fromJson(data);
        controller.add(update);
      } catch (e) {
        debugPrint('Failed to parse ride update: $e');
      }
    });

    return controller.stream;
  }

  @override
  void unsubscribeFromRideUpdates(String rideId) {
    final controller = _subscriptions.remove(rideId);
    if (controller != null) {
      controller.close();
      _webSocketClient.unsubscribe('ride:$rideId');
    }
  }

  // ==========================================================================
  // ACTIVE RIDE / RESULT-BASED CANCEL & RATE
  // ==========================================================================

  @override
  Future<Ride?> getActiveRide() async {
    try {
      final response =
          await _apiClient.get('/api/v1/nidus/rides/active');
      final data = response.data;
      if (data == null) return null;
      return Ride.fromJson(data as Map<String, dynamic>);
    } on ApiException catch (e) {
      // 404 simply means "no active ride" — not an error worth surfacing.
      if (e.statusCode == 404) return null;
      debugPrint('Failed to get active ride: $e');
      return null;
    } catch (e) {
      debugPrint('Failed to get active ride: $e');
      return null;
    }
  }

  @override
  Future<CancelRideResult> cancelRideWithResult(
    String rideId, {
    String? reason,
  }) async {
    try {
      final response = await _apiClient.post(
        '/api/v1/nidus/rides/$rideId/cancel',
        data: {'reason': reason},
      );

      final data = response.data;
      double fee = 0;
      String currency = 'ETB';
      if (data is Map<String, dynamic>) {
        fee = data['cancellation_fee'] != null
            ? (data['cancellation_fee'] as num).toDouble()
            : 0;
        currency = data['currency'] as String? ?? 'ETB';
      }

      return CancelRideResult.cancelled(
        cancellationFee: fee,
        currency: currency,
      );
    } on ApiException catch (e) {
      return CancelRideResult.failure(
        errorCode: e.errorCode ?? 'CANCEL_FAILED',
        message: e.message,
      );
    } catch (e) {
      return CancelRideResult.failure(
        errorCode: 'NETWORK_ERROR',
        message: 'Failed to cancel ride: ${e.toString()}',
      );
    }
  }

  @override
  Future<RateRideResult> rateRideWithResult(
    String rideId,
    int rating, {
    String? review,
    double? tip,
  }) async {
    try {
      await _apiClient.post(
        '/api/v1/nidus/rides/$rideId/rate',
        data: {
          'rating': rating,
          'review': review,
          'tip': tip,
        },
      );
      return const RateRideResult.rated();
    } on ApiException catch (e) {
      return RateRideResult.failure(
        errorCode: e.errorCode ?? 'RATE_FAILED',
        message: e.message,
      );
    } catch (e) {
      return RateRideResult.failure(
        errorCode: 'NETWORK_ERROR',
        message: 'Failed to rate ride: ${e.toString()}',
      );
    }
  }

  // ==========================================================================
  // CLEANUP
  // ==========================================================================

  void dispose() {
    for (final controller in _subscriptions.values) {
      controller.close();
    }
    _subscriptions.clear();
  }
}