import 'package:dio/dio.dart';

import '../../../../core/network/api_client.dart';
import '../../../../core/network/websocket_client.dart';
import '../../domain/entities/active_ride.dart';
import '../../domain/entities/pending_ride_offer.dart';

/// Driver-side ride repository.
///
/// Every path below is verified against the backend router
/// (backend/internal/modules/nidus/interfaces/http/router.go):
///  * POST /api/v1/nidus/rides/{rideID}/accept   -> RideResponse
///  * POST /api/v1/nidus/rides/{rideID}/start    -> RideResponse
///  * POST /api/v1/nidus/rides/{rideID}/complete -> CompleteRideResponse
///  * POST /api/v1/nidus/rides/{rideID}/cancel   -> RideResponse
///  * GET  /api/v1/nidus/drivers/active-ride     -> RideResponse | 404 ACTIVE_RIDE_NOT_FOUND
///  * GET  /api/v1/nidus/drivers/pending-rides   -> {rides:[PendingRideOffer], total:int}
abstract class RideRepository {
  Future<ActiveRide> acceptRide(String rideId);
  Future<ActiveRide> startRide(String rideId);
  Future<ActiveRide> completeRide(String rideId, double tipAmount);
  /// Null when the driver holds no live assignment (404 is an honest empty
  /// state, mirroring the rider app's getActiveRide convention).
  Future<ActiveRide?> getActiveRide();
  Future<List<PendingRideOffer>> getPendingRides();
  Future<void> cancelRide(String rideId, String reason);

  /// Fires after every successful WebSocket (re)connect. Null when the
  /// transport does not expose reconnect signals.
  Stream<bool>? get connectionReconnects;

  /// Partial WS `ride_update` payloads keyed to the current authoritative
  /// ride. [base] is the REST-loaded ride used for merge precedence so a
  /// reconnecting driver never loses fields the event does not carry.
  Stream<ActiveRide> getRideUpdates(ActiveRide? base);
}

class RideRepositoryImpl implements RideRepository {
  final ApiClient _apiClient;
  final WebSocketClient _webSocketClient;

  /// Topic the repository keeps server-subscribed for the currently tracked
  /// ride (backend hub: {"type":"subscribe","payload":{"topic":...}}).
  String? _subscribedRideTopic;

  RideRepositoryImpl({
    required ApiClient apiClient,
    required WebSocketClient webSocketClient,
  })  : _apiClient = apiClient,
        _webSocketClient = webSocketClient;

  @override
  Future<ActiveRide> acceptRide(String rideId) async {
    try {
      final response = await _apiClient.post(
        '/api/v1/nidus/rides/$rideId/accept',
      );
      final data = response.data as Map<String, dynamic>;
      subscribeToRide(rideId);
      return ActiveRide.fromJson(data);
    } catch (e) {
      throw Exception('Failed to accept ride: ${e.toString()}');
    }
  }

  @override
  Future<ActiveRide> startRide(String rideId) async {
    try {
      final response = await _apiClient.post(
        '/api/v1/nidus/rides/$rideId/start',
      );
      final data = response.data as Map<String, dynamic>;
      subscribeToRide(rideId);
      return ActiveRide.fromJson(data);
    } catch (e) {
      throw Exception('Failed to start ride: ${e.toString()}');
    }
  }

  /// Completes the ride and projects the authoritative
  /// [CompleteRideResponse] money breakdown onto the pre-completion ride.
  /// The endpoint returns the settlement view (final_fare/tip), NOT a
  /// RideResponse — parsing it as one would fabricate coordinates, so we
  /// merge explicitly.
  @override
  Future<ActiveRide> completeRide(String rideId, double tipAmount) async {
    ActiveRide? previous;
    try {
      // Best-effort snapshot for the terminal projection; if this fails the
      // completion itself still proceeds against the server as authority.
      previous = await getActiveRide();
    } catch (_) {
      previous = null;
    }

    try {
      final response = await _apiClient.post(
        '/api/v1/nidus/rides/$rideId/complete',
        data: {'tip_amount': tipAmount},
      );
      final data = response.data as Map<String, dynamic>;
      final projected = ActiveRide(
        id: (data['ride_id'] ?? previous?.id ?? rideId) as String,
        riderId: previous?.riderId ?? '',
        riderName: previous?.riderName,
        riderPhone: previous?.riderPhone,
        riderRating: previous?.riderRating,
        pickupLat: previous?.pickupLat ?? 0,
        pickupLng: previous?.pickupLng ?? 0,
        pickupAddress: previous?.pickupAddress ?? '',
        dropoffLat: previous?.dropoffLat ?? 0,
        dropoffLng: previous?.dropoffLng ?? 0,
        dropoffAddress: previous?.dropoffAddress ?? '',
        distanceKm: previous?.distanceKm,
        durationMinutes: previous?.durationMinutes,
        // Authoritative settled numbers from CompleteRideResponse.
        fareAmount: (data['final_fare'] as num?)?.toDouble(),
        tipAmount: (data['tip'] as num?)?.toDouble() ?? tipAmount,
        status: (data['status'] as String?) ?? 'completed',
        requestedAt: previous?.requestedAt ?? DateTime.now(),
        acceptedAt: previous?.acceptedAt,
        startedAt: previous?.startedAt,
        completedAt: DateTime.now(),
        riderNote: previous?.riderNote,
        matchedEtaMinutes: previous?.matchedEtaMinutes,
        currency: previous?.currency ?? '',
      );
      unsubscribeFromRide(rideId);
      return projected;
    } on DioException catch (e) {
      if (e.response?.statusCode == 409 &&
          _isAlreadyCompleted(e.response?.data)) {
        // Idempotent replay: the ride already completed server-side. Do not
        // fake money numbers — surface the conflict honestly.
        throw Exception(
            'Ride already completed on the server (idempotent replay rejected)');
      }
      throw Exception('Failed to complete ride: ${e.toString()}');
    } catch (e) {
      throw Exception('Failed to complete ride: ${e.toString()}');
    }
  }

  static bool _isAlreadyCompleted(Object? body) {
    if (body is Map && body['error'] is Map) {
      final code = (body['error'] as Map)['code'];
      return code == 'INVALID_TRANSITION' || code == 'RIDE_ALREADY_COMPLETED';
    }
    return false;
  }

  @override
  Future<ActiveRide?> getActiveRide() async {
    try {
      final response = await _apiClient.get('/api/v1/nidus/drivers/active-ride');
      final data = response.data as Map<String, dynamic>;
      final ride = ActiveRide.fromJson(data);
      subscribeToRide(ride.id);
      return ride;
    } on DioException catch (e) {
      // 404 ACTIVE_RIDE_NOT_FOUND simply means "no active ride" — same
      // honest-empty convention the rider app uses.
      if (e.response?.statusCode == 404) return null;
      throw Exception('Failed to get active ride: ${e.toString()}');
    } catch (e) {
      throw Exception('Failed to get active ride: ${e.toString()}');
    }
  }

  @override
  Future<List<PendingRideOffer>> getPendingRides() async {
    try {
      final response =
          await _apiClient.get('/api/v1/nidus/drivers/pending-rides');
      final data = response.data as Map<String, dynamic>;
      final rides = (data['rides'] as List?) ?? const [];
      return rides
          .map((r) => PendingRideOffer.fromJson(r as Map<String, dynamic>))
          .toList();
    } catch (e) {
      throw Exception('Failed to fetch pending rides: ${e.toString()}');
    }
  }

  @override
  Future<void> cancelRide(String rideId, String reason) async {
    try {
      await _apiClient.post(
        '/api/v1/nidus/rides/$rideId/cancel',
        data: {'reason': reason},
      );
      unsubscribeFromRide(rideId);
    } catch (e) {
      throw Exception('Failed to cancel ride: ${e.toString()}');
    }
  }

  @override
  Stream<bool>? get connectionReconnects => _webSocketClient.connectionStateStream;

  // ==========================================================================
  // WEBSOCKET RIDE ROOM
  // ==========================================================================

  /// Asks the hub to join this client to `ride:<id>` (server enforces
  /// ride-level authorization; foreign subscriptions are answered FORBIDDEN).
  void subscribeToRide(String rideId) {
    final topic = 'ride:$rideId';
    if (_subscribedRideTopic == topic) return;
    if (_subscribedRideTopic != null) {
      _webSocketClient.unsubscribe(_subscribedRideTopic!);
    }
    _subscribedRideTopic = topic;
    _webSocketClient.subscribe(topic);
  }

  void unsubscribeFromRide(String rideId) {
    final topic = 'ride:$rideId';
    if (_subscribedRideTopic == topic) {
      _subscribedRideTopic = null;
    }
    _webSocketClient.unsubscribe(topic);
  }

  @override
  Stream<ActiveRide> getRideUpdates(ActiveRide? base) {
    // Wire format (ws_bridge.go): Message{Type:"ride_update",
    // Payload:{topic, data:{ride_id,status,...}}, Timestamp}.
    return _webSocketClient.messageStream
        .where((message) => message['type'] == 'ride_update')
        .map((message) {
          final payload = message['payload'];
          if (payload is! Map) return null;
          final data = payload['data'];
          if (data is! Map) return null;
          final update = Map<String, dynamic>.from(data);
          final rideId = (update['ride_id'] ?? update['id']) as String?;
          if (rideId == null) return null;
          if (base != null && base.id == rideId) {
            // Merge onto the authoritative REST snapshot: partial events
            // advance only the fields they actually carry.
            return base.mergeUpdate(update);
          }
          if (base != null && base.id != rideId) {
            // Event for a different ride — stale/foreign, drop it here so
            // every consumer does not have to re-filter.
            return null;
          }
          // No cached base: only trust a self-contained full payload.
          if (update.containsKey('status')) {
            return ActiveRide.fromJson(update);
          }
          return null;
        })
        .where((ride) => ride != null)
        .cast<ActiveRide>();
  }
}
