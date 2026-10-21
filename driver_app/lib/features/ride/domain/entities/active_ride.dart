import 'package:equatable/equatable.dart';

/// Driver-side projection of the authoritative ride state.
///
/// JSON contract source of truth (verified against the repository):
///  * REST: backend/internal/modules/nidus/interfaces/http/handlers/
///    ride_handler.go — `RideResponse` (GET /nidus/drivers/active-ride,
///    POST /nidus/rides/{id}/accept|start) and `CompleteRideResponse`
///    (POST /nidus/rides/{id}/complete).
///  * WebSocket: handlers/ws_bridge.go — `{type:"ride_update",
///    payload:{topic, data:{ride_id,status,...}}}` partial payloads.
///
/// Nullability policy (Global Rule 2): a nullable field here means the
/// server genuinely omitted it (`omitempty` pointer). Consumers must render
/// an honest "not provided" state — never zero-fill or fabricate. In
/// particular the REST `RideResponse` carries NO rider-profile join, so
/// [riderName]/[riderPhone]/[riderRating] are null on rehydration; only the
/// WS offer payload can supply them.
class ActiveRide extends Equatable {
  final String id;
  final String riderId;
  final String? riderName;
  final String? riderPhone;
  final double? riderRating;
  final double pickupLat;
  final double pickupLng;
  final String pickupAddress;
  final double dropoffLat;
  final double dropoffLng;
  final String dropoffAddress;
  final double? distanceKm;
  final int? durationMinutes;
  final double? fareAmount;
  final double tipAmount;
  final String status;
  final DateTime requestedAt;
  final DateTime? acceptedAt;
  final DateTime? startedAt;
  final DateTime? completedAt;
  final String? riderNote;
  /// The assigned driver's actual computed pickup ETA persisted atomically
  /// with the assignment (rides.matched_eta_minutes, NIDUS-DISPATCH-001).
  /// Null when the row predates migration 014 or the dispatcher recorded no
  /// ETA — clients fall back to the live driver ETA, never a constant.
  final int? matchedEtaMinutes;
  final String currency;

  const ActiveRide({
    required this.id,
    required this.riderId,
    this.riderName,
    this.riderPhone,
    this.riderRating,
    required this.pickupLat,
    required this.pickupLng,
    required this.pickupAddress,
    required this.dropoffLat,
    required this.dropoffLng,
    required this.dropoffAddress,
    this.distanceKm,
    this.durationMinutes,
    this.fareAmount,
    this.tipAmount = 0,
    required this.status,
    required this.requestedAt,
    this.acceptedAt,
    this.startedAt,
    this.completedAt,
    this.riderNote,
    this.matchedEtaMinutes,
    this.currency = '',
  });

  static double? _asDouble(Object? v) => v is num ? v.toDouble() : null;
  static int? _asInt(Object? v) => v is num ? v.toInt() : null;
  static DateTime? _asDate(Object? v) =>
      v is String ? DateTime.tryParse(v) : null;

  /// Parses a full `RideResponse` (REST) or a merged WS `ride_update` data
  /// object. Coordinates are read under both the REST names (`pickup_lat`)
  /// and the event-payload names (`pickup_latitude`) emitted by the bus
  /// publishers, whichever is present; absent coordinates only ever occur in
  /// partial WS updates that are then merged via [mergeUpdate].
  factory ActiveRide.fromJson(Map<String, dynamic> json) {
    return ActiveRide(
      id: (json['id'] ?? json['ride_id']) as String,
      riderId: (json['user_id'] ?? json['rider_id']) as String? ?? '',
      riderName: (json['rider_name'] ?? json['user_name']) as String?,
      riderPhone: (json['rider_phone'] ?? json['user_phone']) as String?,
      riderRating: _asDouble(json['rider_rating'] ?? json['user_rating']),
      pickupLat: _asDouble(json['pickup_lat'] ?? json['pickup_latitude']) ?? 0,
      pickupLng: _asDouble(json['pickup_lng'] ?? json['pickup_longitude']) ?? 0,
      pickupAddress: (json['pickup_address'] as String?) ?? '',
      dropoffLat:
          _asDouble(json['dropoff_lat'] ?? json['dropoff_latitude']) ?? 0,
      dropoffLng:
          _asDouble(json['dropoff_lng'] ?? json['dropoff_longitude']) ?? 0,
      dropoffAddress: (json['dropoff_address'] as String?) ?? '',
      distanceKm: _asDouble(json['distance_km']),
      durationMinutes: _asInt(json['duration_minutes']),
      fareAmount: _asDouble(json['fare_amount']),
      tipAmount: _asDouble(json['tip_amount']) ?? 0,
      status: (json['status'] as String?) ?? '',
      requestedAt: _asDate(json['requested_at']) ?? DateTime.now(),
      acceptedAt: _asDate(json['accepted_at'] ?? json['matched_at']),
      startedAt: _asDate(json['started_at']),
      completedAt: _asDate(json['completed_at']),
      riderNote: (json['rider_note'] ?? json['notes']) as String?,
      matchedEtaMinutes: _asInt(json['matched_eta_minutes']),
      currency: (json['currency'] as String?) ?? '',
    );
  }

  Map<String, dynamic> toJson() => {
        'id': id,
        'user_id': riderId,
        if (riderName != null) 'rider_name': riderName,
        if (riderPhone != null) 'rider_phone': riderPhone,
        if (riderRating != null) 'rider_rating': riderRating,
        'pickup_lat': pickupLat,
        'pickup_lng': pickupLng,
        'pickup_address': pickupAddress,
        'dropoff_lat': dropoffLat,
        'dropoff_lng': dropoffLng,
        'dropoff_address': dropoffAddress,
        if (distanceKm != null) 'distance_km': distanceKm,
        if (durationMinutes != null) 'duration_minutes': durationMinutes,
        if (fareAmount != null) 'fare_amount': fareAmount,
        'tip_amount': tipAmount,
        'status': status,
        'requested_at': requestedAt.toIso8601String(),
        if (acceptedAt != null) 'accepted_at': acceptedAt!.toIso8601String(),
        if (startedAt != null) 'started_at': startedAt!.toIso8601String(),
        if (completedAt != null)
          'completed_at': completedAt!.toIso8601String(),
        if (riderNote != null) 'rider_note': riderNote,
        if (matchedEtaMinutes != null) 'matched_eta_minutes': matchedEtaMinutes,
        'currency': currency,
      };

  /// Merges a partial WS `ride_update` payload (ws_bridge.go `data` map:
  /// always carries `ride_id`, `status`, `type`, `event_ts`, plus whatever
  /// the domain event included) onto this cached authoritative ride.
  /// Unknown/partial fields keep their REST-loaded values — the REST
  /// response stays authoritative, events only advance what they carry.
  ActiveRide mergeUpdate(Map<String, dynamic> update) {
    final updStatus = update['status'] as String?;
    final merged = ActiveRide(
      id: (update['ride_id'] ?? update['id']) as String? ?? id,
      riderId: (update['user_id'] ?? update['rider_id']) as String? ?? riderId,
      riderName: (update['rider_name']) as String? ?? riderName,
      riderPhone: (update['rider_phone']) as String? ?? riderPhone,
      riderRating: _asDouble(update['rider_rating']) ?? riderRating,
      pickupLat: _asDouble(update['pickup_lat'] ?? update['pickup_latitude']) ?? pickupLat,
      pickupLng: _asDouble(update['pickup_lng'] ?? update['pickup_longitude']) ?? pickupLng,
      pickupAddress: (update['pickup_address'] as String?) ?? pickupAddress,
      dropoffLat: _asDouble(update['dropoff_lat'] ?? update['dropoff_latitude']) ?? dropoffLat,
      dropoffLng: _asDouble(update['dropoff_lng'] ?? update['dropoff_longitude']) ?? dropoffLng,
      dropoffAddress: (update['dropoff_address'] as String?) ?? dropoffAddress,
      distanceKm: _asDouble(update['distance_km']) ?? distanceKm,
      durationMinutes: _asInt(update['duration_minutes']) ?? durationMinutes,
      fareAmount: _asDouble(update['fare_amount']) ?? fareAmount,
      tipAmount: _asDouble(update['tip_amount']) ?? tipAmount,
      status: updStatus ?? status,
      requestedAt: _asDate(update['requested_at']) ?? requestedAt,
      acceptedAt: _asDate(update['accepted_at'] ?? update['matched_at']) ?? acceptedAt,
      startedAt: _asDate(update['started_at']) ?? startedAt,
      completedAt: _asDate(update['completed_at']) ?? completedAt,
      riderNote: (update['rider_note'] ?? update['notes']) as String? ?? riderNote,
      // ETA precedence chain shared with the rider app:
      // live event eta ?? persisted matched ETA from REST ?? keep current.
      matchedEtaMinutes: _asInt(
              update['matched_eta_minutes'] ?? update['eta_minutes']) ??
          matchedEtaMinutes,
      currency: (update['currency'] as String?) ?? currency,
    );
    return merged;
  }

  ActiveRide copyWith({
    String? status,
    DateTime? acceptedAt,
    DateTime? startedAt,
    DateTime? completedAt,
    double? fareAmount,
    double? tipAmount,
    int? matchedEtaMinutes,
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
      durationMinutes: durationMinutes,
      fareAmount: fareAmount ?? this.fareAmount,
      tipAmount: tipAmount ?? this.tipAmount,
      status: status ?? this.status,
      requestedAt: requestedAt,
      acceptedAt: acceptedAt ?? this.acceptedAt,
      startedAt: startedAt ?? this.startedAt,
      completedAt: completedAt ?? this.completedAt,
      riderNote: riderNote,
      matchedEtaMinutes: matchedEtaMinutes ?? this.matchedEtaMinutes,
      currency: currency,
    );
  }

  // ---------------------------------------------------------------------
  // Backend lifecycle vocabulary (entities.RideStatus, ride.go):
  // requested | searching | matched | driver_en_route | in_progress |
  // completed | paid | cancelled | no_driver_available
  // ---------------------------------------------------------------------

  bool get isMatched => status == 'matched';
  bool get isEnRoute => status == 'driver_en_route';
  bool get isInProgress => status == 'in_progress';
  bool get isCompleted => status == 'completed' || status == 'paid';
  bool get isCancelled => status == 'cancelled' || status == 'no_driver_available';
  bool get isTerminal => isCompleted || isCancelled;

  double get totalEarnings => (fareAmount ?? 0) + tipAmount;

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
        tipAmount,
        matchedEtaMinutes,
        completedAt,
      ];
}
