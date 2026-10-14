import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';

import '../../../../core/network/api_client.dart';
import '../../../../core/network/websocket_client.dart';
import 'pre_selfie_repository.dart';

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

  /// §2.6: per-type ride-option estimates for the request carousel.
  ///
  /// Calls the canonical `GET /api/v1/nidus/rides/estimates?pickup=..&dropoff=..`
  /// surface and falls back to the legacy POST `/rides/estimate` (same wire
  /// shape) when the GET is unavailable. Returns one entry per ride type the
  /// pricing service produced — including Shared and Wheelchair. Throws
  /// [Exception] when both surfaces fail; callers must render a retryable
  /// "Prices unavailable" state, never hardcoded cards.
  Future<List<RideOptionEstimate>> getRideOptionEstimates({
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

  /// §2.5: fetch the authoritative cancellation-fee quote for a ride.
  ///
  /// The backend computes this with the same policy service that charges the
  /// fee on CancelRide, so the number shown here is the number the rider
  /// pays if they cancel now. Throws [ApiException] when the quote cannot be
  /// retrieved; callers must degrade honestly ("Fee will be confirmed on
  /// cancel") and never invent a number. Rides in terminal states fail with
  /// errorCode `RIDE_NOT_CANCELLABLE`.
  Future<CancellationQuote> getCancellationQuote(String rideId);

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

  /// §2.3 driver pre-selfie verification state, embedded by the backend on
  /// rider ride DTOs when FEATURE_PRE_SELFIE is on. Null (or
  /// [PreSelfieInfo.absent]) means "verification unavailable" — an honest
  /// retry state, never a fabricated photo or URL.
  final PreSelfieInfo? preSelfie;

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
    this.preSelfie,
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
      preSelfie: json['pre_selfie'] != null
          ? PreSelfieInfo.fromJson(json['pre_selfie'] as Map<String, dynamic>)
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

/// §2.6: one per-ride-type estimate for the ride-option carousel. Mirrors the
/// backend `FareEstimateResponse`. Every field is server-sourced — display
/// name, capacity, price band, ETA, surge badge and availability. The client
/// renders zero hardcoded cards; when a legacy response omits the new fields
/// they degrade to null (rendered as honest "unavailable" text), never to a
/// fabricated value.
class RideOptionEstimate {
  final String rideType;
  final String? displayName;
  final String? description;
  final int? capacity;
  final String? iconKey;
  final double? priceLow;
  final double? priceHigh;
  final double totalFare;
  final double surgeMultiplier;
  final String? surgeReason;
  final String currency;
  final int etaMinutes;
  final bool available;

  const RideOptionEstimate({
    required this.rideType,
    this.displayName,
    this.description,
    this.capacity,
    this.iconKey,
    this.priceLow,
    this.priceHigh,
    required this.totalFare,
    required this.surgeMultiplier,
    this.surgeReason,
    required this.currency,
    required this.etaMinutes,
    required this.available,
  });

  factory RideOptionEstimate.fromJson(Map<String, dynamic> json) {
    double? asDouble(String key) {
      final v = json[key];
      return v is num ? v.toDouble() : null;
    }

    int? asInt(String key) {
      final v = json[key];
      return v is num ? v.toInt() : null;
    }

    return RideOptionEstimate(
      rideType: json['ride_type'] as String? ?? 'standard',
      displayName: json['display_name'] as String?,
      description: json['description'] as String?,
      capacity: asInt('capacity'),
      iconKey: json['icon_key'] as String?,
      // Legacy responses lack the price band; fall back to the point fare so
      // the card shows a real number derived from the pricing service.
      priceLow: asDouble('price_low'),
      priceHigh: asDouble('price_high'),
      totalFare: (json['total_fare'] as num?)?.toDouble() ?? 0,
      surgeMultiplier: (json['surge_multiplier'] as num?)?.toDouble() ?? 1.0,
      surgeReason: json['surge_reason'] as String?,
      currency: json['currency'] as String? ?? 'ETB',
      etaMinutes: asInt('estimated_pickup_minutes') ?? 0,
      available: json['available'] as bool? ?? true,
    );
  }

  /// Display price band. Prefers the server-provided low/high; degrades to
  /// the point estimate. All values originate from the pricing service —
  /// nothing here is a hardcoded "$12–15".
  String get priceRangeLabel {
    final low = priceLow ?? totalFare;
    final high = priceHigh ?? totalFare;
    if ((high - low).abs() < 0.005) {
      return '${currency == 'USD' ? '\$' : '$currency '}${low.toStringAsFixed(2)}';
    }
    final symbol = currency == 'USD' ? '\$' : '$currency ';
    return '$symbol${low.toStringAsFixed(0)}–$symbol${high.toStringAsFixed(0)}';
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

  /// Live ETA in minutes pushed by the matching / ETA services, when present.
  final int? etaMinutes;

  /// §2.3 pre-selfie state carried on a WS status payload, when present.
  /// Most deployments embed it only on the REST ride DTO (LoadRide), so this
  /// is usually null and the bloc keeps the last-known value from REST.
  final PreSelfieInfo? preSelfie;

  const RideStatusUpdate({
    required this.rideId,
    required this.status,
    this.driver,
    this.driverLat,
    this.driverLng,
    required this.timestamp,
    this.cancelledBy,
    this.cancelReason,
    this.etaMinutes,
    this.preSelfie,
  });

  factory RideStatusUpdate.fromJson(Map<String, dynamic> json) {
    // The WS bridge may deliver either a bare payload or one wrapped in an
    // envelope ({type/event, data:{...}}); unwrap defensively so neither
    // shape crashes the parser.
    var payload = json;
    final data = json['data'];
    if (data is Map<String, dynamic>) {
      payload = data;
    }

    DriverInfo? parseDriver(Map<String, dynamic>? raw) {
      if (raw == null) return null;
      try {
        return DriverInfo.fromJson(raw);
      } catch (e) {
        debugPrint('Failed to parse driver payload: $e');
        return null;
      }
    }

    final driverJson = payload['driver'];
    final timestampRaw = payload['timestamp'];

    return RideStatusUpdate(
      rideId: (payload['ride_id'] ?? payload['id']) as String,
      status: payload['status'] as String,
      driver: parseDriver(driverJson is Map<String, dynamic>
          ? driverJson
          : null),
      driverLat: payload['driver_lat'] != null
          ? (payload['driver_lat'] as num).toDouble()
          : null,
      driverLng: payload['driver_lng'] != null
          ? (payload['driver_lng'] as num).toDouble()
          : null,
      timestamp: timestampRaw is String
          ? DateTime.tryParse(timestampRaw) ?? DateTime.now()
          : DateTime.now(),
      cancelledBy: payload['cancelled_by'] as String?,
      cancelReason: payload['reason'] as String?,
      etaMinutes: payload['eta_minutes'] is num
          ? (payload['eta_minutes'] as num).toInt()
          : null,
      preSelfie: payload['pre_selfie'] is Map<String, dynamic>
          ? PreSelfieInfo.fromJson(payload['pre_selfie'] as Map<String, dynamic>)
          : null,
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

/// §2.5: authoritative cancellation-fee quote returned by
/// GET /api/v1/nidus/rides/{rideId}/cancellation-quote. Mirrors the backend's
/// services.CancellationQuote exactly; the UI renders these values verbatim.
class CancellationQuoteBreakdownItem {
  final String label;
  final double amount;
  final String currency;

  const CancellationQuoteBreakdownItem({
    required this.label,
    required this.amount,
    required this.currency,
  });

  factory CancellationQuoteBreakdownItem.fromJson(Map<String, dynamic> json) {
    return CancellationQuoteBreakdownItem(
      label: json['label'] as String? ?? '',
      amount: (json['amount'] as num?)?.toDouble() ?? 0,
      currency: json['currency'] as String? ?? 'ETB',
    );
  }
}

class CancellationQuote {
  final String rideId;
  final double fee;
  final String currency;

  /// FREE_WINDOW | NO_DRIVER_YET | DRIVER_ARRIVED | TRIP_STARTED |
  /// DRIVER_CANCELLED — machine-readable classification from the policy.
  final String reasonCode;

  /// Whole seconds left before the post-match free window closes. The sheet
  /// ticks this down locally instead of re-fetching.
  final int freeWindowSecondsRemaining;
  final List<CancellationQuoteBreakdownItem> breakdown;
  final DateTime computedAt;
  final DateTime expiresAt;
  final bool fromCache;

  const CancellationQuote({
    required this.rideId,
    required this.fee,
    required this.currency,
    required this.reasonCode,
    required this.freeWindowSecondsRemaining,
    required this.breakdown,
    required this.computedAt,
    required this.expiresAt,
    required this.fromCache,
  });

  bool get isFree => fee <= 0;

  factory CancellationQuote.fromJson(Map<String, dynamic> json) {
    return CancellationQuote(
      rideId: json['ride_id'] as String? ?? '',
      fee: (json['fee'] as num?)?.toDouble() ?? 0,
      currency: json['currency'] as String? ?? 'ETB',
      reasonCode: json['reason_code'] as String? ?? '',
      freeWindowSecondsRemaining:
          (json['free_window_seconds_remaining'] as num?)?.toInt() ?? 0,
      breakdown: (json['breakdown'] as List<dynamic>? ?? const [])
          .whereType<Map<String, dynamic>>()
          .map(CancellationQuoteBreakdownItem.fromJson)
          .toList(growable: false),
      computedAt:
          DateTime.tryParse(json['computed_at'] as String? ?? '') ??
              DateTime.now().toUtc(),
      expiresAt:
          DateTime.tryParse(json['expires_at'] as String? ?? '') ??
              DateTime.now().toUtc(),
      fromCache: json['from_cache'] as bool? ?? false,
    );
  }
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
      final response = await _apiClient.post<Map<String, dynamic>>(
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

      final body = response.data;
      if (body == null) {
        return const RideResult.failure(
          error: 'Server returned an empty response',
          errorCode: 'EMPTY_RESPONSE',
        );
      }

      // Unwrap a possible {"data": {...}} envelope and accept both the ride
      // id key spellings emitted by the handler.
      var data = body;
      final inner = body['data'];
      if (inner is Map<String, dynamic>) data = inner;

      final rideId = (data['ride_id'] ?? data['id']) as String?;
      final status = data['status'] as String?;
      final fareRaw = data['fare'] ?? data['fare_amount'];
      final etaRaw = data['eta'] ?? data['eta_minutes'];

      if (rideId == null || fareRaw is! num) {
        return RideResult.failure(
          error: 'Unexpected ride response shape: ${body.toString()}',
          errorCode: 'INVALID_RESPONSE',
        );
      }

      return RideResult.success(
        rideId: rideId,
        status: status ?? 'requested',
        fare: fareRaw.toDouble(),
        eta: etaRaw is num ? etaRaw.toInt() : 0,
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

  @override
  Future<List<RideOptionEstimate>> getRideOptionEstimates({
    required double pickupLat,
    required double pickupLng,
    required double dropoffLat,
    required double dropoffLng,
  }) async {
    // Canonical GET surface registered in the router (§2.6).
    try {
      final response = await _apiClient.get(
        '/api/v1/nidus/rides/estimates',
        queryParameters: {
          'pickup': '$pickupLat,$pickupLng',
          'dropoff': '$dropoffLat,$dropoffLng',
        },
      );
      final parsed = _parseEstimateEnvelope(response.data);
      if (parsed != null) return parsed;
    } catch (e) {
      debugPrint('GET /rides/estimates failed, trying legacy POST: $e');
    }

    // Legacy POST fallback — same wire envelope, older per-card fields.
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
      final parsed = _parseEstimateEnvelope(response.data);
      if (parsed != null) return parsed;
    } catch (e) {
      debugPrint('POST /rides/estimate also failed: $e');
    }

    throw Exception('fare estimates unavailable');
  }

  List<RideOptionEstimate>? _parseEstimateEnvelope(dynamic rawData) {
    Map<String, dynamic>? map;
    if (rawData is Map<String, dynamic>) {
      map = rawData;
    } else if (rawData is String) {
      final decoded = jsonDecode(rawData);
      if (decoded is Map<String, dynamic>) map = decoded;
    }
    if (map == null) return null;
    final list = map['estimates'];
    if (list is! List) return null;
    return list
        .whereType<Map<String, dynamic>>()
        .map(RideOptionEstimate.fromJson)
        .toList();
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

    // Subscribe to WebSocket. The hub delivers decoded maps whose payload
    // lives under `data` (see ws_bridge wire shape), so unwrap before parsing.
    _webSocketClient.subscribe('ride:$rideId', (message) {
      try {
        final Map<String, dynamic>? envelope;
        if (message is Map<String, dynamic>) {
          envelope = message;
        } else if (message is String) {
          final decoded = jsonDecode(message);
          envelope = decoded is Map<String, dynamic> ? decoded : null;
        } else {
          envelope = null;
        }
        if (envelope == null) return;

        final inner = envelope['data'];
        final payload = inner is Map<String, dynamic> ? inner : envelope;

        controller.add(RideStatusUpdate.fromJson(payload));
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
  Future<CancellationQuote> getCancellationQuote(String rideId) async {
    final response = await _apiClient.get<Map<String, dynamic>>(
      '/api/v1/nidus/rides/$rideId/cancellation-quote',
    );
    var data = response.data ?? const <String, dynamic>{};
    final inner = data['data'];
    if (inner is Map<String, dynamic>) data = inner;
    return CancellationQuote.fromJson(data);
  }

  @override
  Future<CancelRideResult> cancelRideWithResult(
    String rideId, {
    String? reason,
  }) async {
    try {
      final response = await _apiClient.post<Map<String, dynamic>>(
        '/api/v1/nidus/rides/$rideId/cancel',
        data: {'reason': reason},
      );

      // The handler returns the full RideResponse; fee fields are additive
      // and default to zero when the backend does not send them yet.
      var data = response.data ?? const <String, dynamic>{};
      final inner = data['data'];
      if (inner is Map<String, dynamic>) data = inner;

      double fee = 0;
      final feeRaw = data['cancellation_fee'] ?? data['cancel_fee'];
      if (feeRaw is num) fee = feeRaw.toDouble();
      final currency = data['currency'] as String? ?? 'ETB';

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