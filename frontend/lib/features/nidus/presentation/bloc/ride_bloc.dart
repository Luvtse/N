import 'dart:async';
import 'dart:math' as math show cos, pi, sqrt;

import 'package:flutter/foundation.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:equatable/equatable.dart';
import 'package:latlong2/latlong.dart';

import '../../../../core/network/api_client.dart';
import '../../data/repositories/ride_repository.dart';
import '../../data/repositories/safety_repository.dart';
import '../../domain/usecases/request_ride_usecase.dart';
import '../../domain/usecases/get_ride_status_usecase.dart';

// ============================================================================
// EVENTS
// ============================================================================

abstract class RideEvent extends Equatable {
  const RideEvent();

  @override
  List<Object?> get props => [];
}

/// Request a new ride
class RequestRide extends RideEvent {
  final double pickupLat;
  final double pickupLng;
  final double dropoffLat;
  final double dropoffLng;
  final String? pickupAddress;
  final String? dropoffAddress;
  final String rideType;
  final String? paymentMethodId;

  /// §3.1: registry rail type ('wallet' | 'card' | 'cash') paired with
  /// [paymentMethodId]. Null means the server resolves the rider's default.
  final String? paymentMethodType;

  /// §3.2: promo code validated by POST /promotions/redeem before booking.
  final String? promoCode;

  /// §3.6: scheduled pickup time (local); null = on-demand. The use case
  /// serializes it to RFC3339 UTC and the backend enforces the ≥15 min /
  /// ≤30 day window, returning SCHEDULED_TOO_SOON / SCHEDULED_TOO_FAR.
  final DateTime? scheduledAt;

  /// §3.5: intermediate waypoints in visit order (max 3, enforced client-side
  /// in the use case with the same wording as the server's TOO_MANY_STOPS).
  /// Empty for direct trips — the repository then omits the `stops` key so
  /// the request body stays byte-identical to a pre-stopovers request.
  final List<RideStop> stops;

  const RequestRide({
    required this.pickupLat,
    required this.pickupLng,
    required this.dropoffLat,
    required this.dropoffLng,
    this.pickupAddress,
    this.dropoffAddress,
    this.rideType = 'standard',
    this.paymentMethodId,
    this.paymentMethodType,
    this.promoCode,
    this.scheduledAt,
    this.stops = const [],
  });

  @override
  List<Object?> get props => [
        pickupLat, pickupLng, dropoffLat, dropoffLng,
        pickupAddress, dropoffAddress, rideType, paymentMethodId,
        paymentMethodType, promoCode, scheduledAt, stops,
      ];
}

/// Load ride details
class LoadRide extends RideEvent {
  final String rideId;

  const LoadRide(this.rideId);

  @override
  List<Object?> get props => [rideId];
}

/// Load ride history
class LoadRideHistory extends RideEvent {
  final String? status;
  final int limit;
  final int offset;

  const LoadRideHistory({
    this.status,
    this.limit = 20,
    this.offset = 0,
  });

  @override
  List<Object?> get props => [status, limit, offset];
}

/// Cancel current ride
class CancelRide extends RideEvent {
  final String rideId;
  final String? reason;

  const CancelRide({
    required this.rideId,
    this.reason,
  });

  @override
  List<Object?> get props => [rideId, reason];
}

/// Subscribe to real-time ride updates
class SubscribeToRideUpdates extends RideEvent {
  final String rideId;

  const SubscribeToRideUpdates(this.rideId);

  @override
  List<Object?> get props => [rideId];
}

/// Unsubscribe from ride updates
class UnsubscribeFromRideUpdates extends RideEvent {
  final String rideId;

  const UnsubscribeFromRideUpdates(this.rideId);

  @override
  List<Object?> get props => [rideId];
}

/// Rate a completed ride
class RateRide extends RideEvent {
  final String rideId;
  final int rating;
  final String? review;
  final double? tip;

  const RateRide({
    required this.rideId,
    required this.rating,
    this.review,
    this.tip,
  });

  @override
  List<Object?> get props => [rideId, rating, review, tip];
}

/// Get fare estimate
class GetFareEstimate extends RideEvent {
  final double pickupLat;
  final double pickupLng;
  final double dropoffLat;
  final double dropoffLng;

  /// §2.6: the rider's currently selected ride type. The estimate is now
  /// computed for THIS type instead of the backend default — previously the
  /// selection was silently ignored by the single-estimate fetch.
  final String? rideType;

  const GetFareEstimate({
    required this.pickupLat,
    required this.pickupLng,
    required this.dropoffLat,
    required this.dropoffLng,
    this.rideType,
  });

  @override
  List<Object?> get props =>
      [pickupLat, pickupLng, dropoffLat, dropoffLng, rideType];
}

/// §2.6: load per-type ride-option estimates for the request carousel.
/// [selectedType] is the rider's current choice; when the refreshed list no
/// longer contains it (or marks it unavailable) the bloc re-anchors to the
/// first available type and emits [RideOptionSelectionChanged] so the page
/// can update its own selection state — fixing the "estimate ignores
/// selected ride type" drift by keeping UI and data in lockstep.
class LoadRideOptions extends RideEvent {
  final double pickupLat;
  final double pickupLng;
  final double dropoffLat;
  final double dropoffLng;
  final String selectedType;

  const LoadRideOptions({
    required this.pickupLat,
    required this.pickupLng,
    required this.dropoffLat,
    required this.dropoffLng,
    this.selectedType = 'standard',
  });

  @override
  List<Object?> get props =>
      [pickupLat, pickupLng, dropoffLat, dropoffLng, selectedType];
}

/// §2.6: rider picked a different option in the carousel. Pure state update —
/// the chosen type is what gets sent with RequestRide.
class SelectRideOption extends RideEvent {
  final String rideType;

  const SelectRideOption(this.rideType);

  @override
  List<Object?> get props => [rideType];
}

/// §2.6: emitted when a refresh forced the selection to change (the previous
/// choice vanished or became unavailable). The page mirrors it into its local
/// selection so the highlighted card always equals the type that will be
/// requested.
class RideOptionSelectionChanged extends RideEvent {
  final String rideType;

  const RideOptionSelectionChanged(this.rideType);

  @override
  List<Object?> get props => [rideType];
}

/// Clear current ride
class ClearCurrentRide extends RideEvent {
  const ClearCurrentRide();
}

/// Begin the SOS pre-alarm countdown. The rider gets [RideBloc.sosCountdownSeconds]
/// to back out before emergency services / trusted contacts are notified.
class StartSosCountdown extends RideEvent {
  final String rideId;
  final double lat;
  final double lng;
  final String? message;

  const StartSosCountdown({
    required this.rideId,
    required this.lat,
    required this.lng,
    this.message,
  });

  @override
  List<Object?> get props => [rideId, lat, lng, message];
}

/// Abort an in-progress SOS countdown (false alarm / accidental hold).
class CancelSosCountdown extends RideEvent {
  const CancelSosCountdown();
}

/// Escalate immediately — skip the remaining countdown and fire the SOS now.
class ConfirmSos extends RideEvent {
  const ConfirmSos();
}

/// §4.5 RideCheck: feeds one driver position sample (from the WS stream or
/// the REST poll) into the anomaly detector. The bloc owns the detection
/// window so every consumer of ride state gets identical behavior; a null
/// `etaConfidence` means the backend did not publish reliability for this
/// update — the confidence trigger stays dormant rather than assuming zero.
class ReportRidePosition extends RideEvent {
  final String rideId;
  final double lat;
  final double lng;
  final DateTime at;
  final double? etaConfidence;

  const ReportRidePosition({
    required this.rideId,
    required this.lat,
    required this.lng,
    required this.at,
    this.etaConfidence,
  });

  @override
  List<Object?> get props => [rideId, lat, lng, at, etaConfidence];
}

/// §4.5 RideCheck: the rider answered an "Is everything OK?" prompt.
/// [ok] true → snooze the detector for a grace period; false is not used
/// (the "Call for help" path fires [StartSosCountdown] instead), but the
/// field keeps the event honest for telemetry.
class DismissRideCheckPrompt extends RideEvent {
  final bool ok;

  const DismissRideCheckPrompt.ok() : ok = true;

  @override
  List<Object?> get props => [ok];
}

// ============================================================================
// STATES
// ============================================================================

abstract class RideState extends Equatable {
  const RideState();

  @override
  List<Object?> get props => [];
}

/// Initial state
class RideInitial extends RideState {
  const RideInitial();
}

/// Loading state
class RideLoading extends RideState {
  const RideLoading();
}

/// Ride requested, waiting for driver
class RideRequested extends RideState {
  final String rideId;
  final double fare;
  final int etaMinutes;
  final DateTime requestedAt;

  /// §3.2: discount the server applied at request time. Null when no promo
  /// was attached — rendered as an absent line, never as "0 saved".
  final double? promoDiscount;

  /// §4.1: real pickup coordinates carried from the request so the tracking
  /// surface can render the pickup pin and offer the AR walk-to-pickup entry
  /// during the pre-match/searching phases. Sourced exclusively from the
  /// rider's own request payload / REST DTO — never fabricated. Null only for
  /// legacy constructions that predate this field.
  final double? pickupLat;
  final double? pickupLng;
  final double? dropoffLat;
  final double? dropoffLng;

  /// §4.1: resolved place names for the pickup/dropoff points, carried from
  /// the rider's own request (or the REST DTO on reload). Null means the
  /// label is genuinely unknown — surfaces show "Pickup point" or resolve it
  /// via reverse geocoding instead of inventing an address.
  final String? pickupName;
  final String? dropoffName;

  /// §4.5 RideCheck: anomaly awaiting the rider's answer during the
  /// searching phase (e.g. a low-confidence ETA echoed with the match
  /// preview). Null when nothing is pending — the common case.
  final RideAnomaly? pendingAnomaly;

  const RideRequested({
    required this.rideId,
    required this.fare,
    required this.etaMinutes,
    required this.requestedAt,
    this.promoDiscount,
    this.pickupLat,
    this.pickupLng,
    this.dropoffLat,
    this.dropoffLng,
    this.pickupName,
    this.dropoffName,
    this.pendingAnomaly,
  });

  @override
  List<Object?> get props => [
        rideId,
        fare,
        etaMinutes,
        requestedAt,
        promoDiscount,
        pickupLat,
        pickupLng,
        dropoffLat,
        dropoffLng,
        pickupName,
        dropoffName,
        pendingAnomaly,
      ];
}

/// Driver matched
class DriverMatched extends RideState {
  final String rideId;
  final DriverInfo driver;
  final double fare;
  final int etaMinutes;

  /// §4.1: real pickup coordinates carried from the live request state /
  /// REST DTO so the tracking surface keeps its pickup pin and the AR
  /// walk-to-pickup entry through the matched phase. Null means genuinely
  /// unavailable — the UI hides the AR button rather than fabricating a
  /// coordinate (Global Rule: no placeholders in shipped surfaces).
  final double? pickupLat;
  final double? pickupLng;
  final double? dropoffLat;
  final double? dropoffLng;

  /// §4.1: resolved pickup place name carried from the request / REST DTO.
  /// Null → the AR entry falls back to reverse geocoding or "Pickup point";
  /// never a fabricated address.
  final String? pickupName;
  final String? dropoffName;

  /// §4.5 RideCheck: anomaly awaiting the rider's answer (see
  /// [RideAnomaly]). Null when nothing is pending — the common case.
  final RideAnomaly? pendingAnomaly;

  const DriverMatched({
    required this.rideId,
    required this.driver,
    required this.fare,
    required this.etaMinutes,
    this.pickupLat,
    this.pickupLng,
    this.dropoffLat,
    this.dropoffLng,
    this.pickupName,
    this.dropoffName,
    this.pendingAnomaly,
  });

  @override
  List<Object?> get props => [
        rideId,
        driver,
        fare,
        pendingAnomaly,
        etaMinutes,
        pickupLat,
        pickupLng,
        dropoffLat,
        dropoffLng,
        pickupName,
        dropoffName,
      ];
}

/// Ride in progress
class RideInProgress extends RideState {
  final Ride ride;

  /// §4.5 RideCheck: the rider's live position when the trip started, used
  /// as the deviation reference while the phone is (usually) with the rider
  /// mid-trip. Sourced from the device GPS stream at in_progress transition
  /// — null when no fresh fix existed, in which case deviation checks stay
  /// dormant rather than comparing against a fabricated origin.
  final LatLng? tripStartOrigin;

  /// §4.5 RideCheck: anomaly awaiting the rider's answer (see
  /// [RideAnomaly]). Null when nothing is pending — the common case.
  final RideAnomaly? pendingAnomaly;

  const RideInProgress(
    this.ride, {
    this.tripStartOrigin,
    this.pendingAnomaly,
  });

  @override
  List<Object?> get props => [ride, tripStartOrigin, pendingAnomaly];
}

/// Ride completed
class RideCompleted extends RideState {
  final Ride ride;

  const RideCompleted(this.ride);

  @override
  List<Object?> get props => [ride];
}

/// Ride cancelled
class RideCancelled extends RideState {
  final String rideId;
  final String reason;

  /// Who ended the ride: 'rider', 'driver', or 'system'.
  final String cancelledBy;

  /// Fee charged for the cancellation, in currency units (0 when waived).
  final double cancellationFee;
  final String currency;

  const RideCancelled({
    required this.rideId,
    required this.reason,
    this.cancelledBy = 'rider',
    this.cancellationFee = 0,
    this.currency = 'ETB',
  });

  bool get wasDriverCancelled => cancelledBy == 'driver';
  bool get wasFreeCancellation => cancellationFee <= 0;

  @override
  List<Object?> get props =>
      [rideId, reason, cancelledBy, cancellationFee, currency];
}

/// §4.5 RideCheck: the reason an "Is everything OK?" prompt fired. Each
/// variant maps 1:1 to a trigger in the plan: [longStop] = vehicle stopped
/// >3 min away from pickup/dropoff, [routeDeviation] = moved >500 m off the
/// expected polyline (or off the straight-line corridor while en route),
/// [lowEtaConfidence] = backend ETA reliability dropped below threshold.
enum RideAnomalyKind { longStop, routeDeviation, lowEtaConfidence }

/// §4.5 RideCheck: a detected anomaly awaiting the rider's answer. Emitted
/// as part of the wrapped ride state so every existing `state is X` check
/// keeps working; the tracking page renders the prompt when
/// `pendingAnomaly != null`. [detailMeters]/[confidence] carry only real
/// measured values — null fields render as absent text, never zeros.
class RideAnomaly {
  final RideAnomalyKind kind;
  final DateTime detectedAt;

  /// How far the checked point deviated, meters. Null for kinds that do
  /// not measure distance.
  final double? detailMeters;

  /// The server-reported ETA confidence that tripped the check, 0.0–1.0.
  /// Null unless [kind] is [RideAnomalyKind.lowEtaConfidence].
  final double? confidence;

  const RideAnomaly({
    required this.kind,
    required this.detectedAt,
    this.detailMeters,
    this.confidence,
  });

  @override
  bool operator ==(Object other) =>
      other is RideAnomaly &&
      other.kind == kind &&
      other.detectedAt == detectedAt &&
      other.detailMeters == detailMeters &&
      other.confidence == confidence;

  @override
  int get hashCode =>
      Object.hash(kind, detectedAt, detailMeters, confidence);
}

/// No drivers found for the request — offer recovery actions.
class NoDriversFound extends RideState {
  final String rideId;

  /// §4.6: the fare that was broadcast when the search failed, so the
  /// price-extension sheet can show real "+10% / +20%" projections derived
  /// from it. Null means the bloc never learned a fare for this ride (e.g.
  /// the WS fired before REST hydrated) — the sheet then shows "current
  /// fare unavailable" and defers every number to the server's response,
  /// never inventing one.
  final double? currentFare;

  /// ISO-4217 code paired with [currentFare]; null → unknown, rendered as
  /// an empty unit rather than a fabricated 'ETB'/'USD'.
  final String? currency;

  const NoDriversFound({
    required this.rideId,
    this.searchedSeconds = 0,
    this.currentFare,
    this.currency,
  });

  @override
  List<Object?> get props => [rideId, searchedSeconds, currentFare, currency];
}

/// Driver is en route to pickup — dedicated arrival state.
class DriverEnRoute extends RideState {
  final String rideId;
  final DriverInfo driver;
  final double fare;
  final int etaMinutes;

  /// PIN the rider shows the driver to start the trip (anti-impersonation).
  final String pinCode;

  /// §2.3 driver pre-selfie verification state embedded on the rider ride
  /// DTO. Null means the backend omitted `pre_selfie` entirely (feature flag
  /// off / attacher unwired) — the UI renders the honest "verification
  /// unavailable" state via PreSelfieInfo.absent, never a placeholder photo.
  final PreSelfieInfo? preSelfie;

  /// §4.1: real pickup coordinates carried from the live request state /
  /// REST DTO so the tracking map keeps its pickup pin and the AR
  /// walk-to-pickup entry point stays wired through the en-route phase.
  /// Null means genuinely unavailable — the AR button hides itself rather
  /// than navigating with a fabricated coordinate.
  final double? pickupLat;
  final double? pickupLng;
  final double? dropoffLat;
  final double? dropoffLng;

  /// §4.1: resolved pickup place name carried from the request / REST DTO.
  /// Null → the AR entry falls back to reverse geocoding or "Pickup point";
  /// never a fabricated address.
  final String? pickupName;
  final String? dropoffName;

  /// §4.5 RideCheck: anomaly awaiting the rider's answer (see
  /// [RideAnomaly]). Null when nothing is pending — the common case.
  final RideAnomaly? pendingAnomaly;

  const DriverEnRoute({
    required this.rideId,
    required this.driver,
    required this.fare,
    required this.etaMinutes,
    required this.pinCode,
    this.preSelfie,
    this.pickupLat,
    this.pickupLng,
    this.dropoffLat,
    this.dropoffLng,
    this.pickupName,
    this.dropoffName,
    this.pendingAnomaly,
  });

  @override
  List<Object?> get props => [
        rideId,
        driver,
        fare,
        etaMinutes,
        pinCode,
        preSelfie,
        pickupLat,
        pickupLng,
        dropoffLat,
        dropoffLng,
        pickupName,
        dropoffName,
        pendingAnomaly,
      ];
}

/// Driver-side mirror of [DriverEnRoute] (§2.3): the same lifecycle stage,
/// but rendered for the signed-in account whose id equals the ride's
/// driver_id. Carries no selfie block because the driver is the one who
/// SENDS it — the capture sheet owns that flow and must never be shown to
/// a rider, which is why the bloc emits distinct types instead of a flag.
class DriverEnRouteForDriver extends RideState {
  final String rideId;
  final double fare;
  final int etaMinutes;

  const DriverEnRouteForDriver({
    required this.rideId,
    required this.fare,
    required this.etaMinutes,
  });

  @override
  List<Object?> get props => [rideId, fare, etaMinutes];
}

/// Payment failed at booking time — recoverable via retry / switch rail.
class PaymentFailed extends RideState {
  final String message;
  final String? errorCode;

  const PaymentFailed({
    required this.message,
    this.errorCode,
  });

  @override
  List<Object?> get props => [message, errorCode];
}

/// A completed ride was successfully rated.
class RideRated extends RideState {
  final String rideId;
  final int rating;
  final double tip;

  const RideRated({
    required this.rideId,
    required this.rating,
    this.tip = 0,
  });

  @override
  List<Object?> get props => [rideId, rating, tip];
}

/// Ride history loaded
class RideHistoryLoaded extends RideState {
  final List<Ride> rides;
  final int total;
  final bool hasMore;

  const RideHistoryLoaded({
    required this.rides,
    required this.total,
    required this.hasMore,
  });

  @override
  List<Object?> get props => [rides, total, hasMore];
}

/// Fare estimate loaded
class FareEstimateLoaded extends RideState {
  final FareEstimate estimate;

  const FareEstimateLoaded(this.estimate);

  @override
  List<Object?> get props => [estimate];
}

/// §2.6: per-type ride-option estimates loaded for the request carousel.
/// The list is exactly what the pricing service returned — every card the UI
/// renders is data-driven (Shared and Wheelchair included). [selectedType]
/// carries the current selection so a refresh that drops the selected type
/// can re-anchor honestly instead of silently keeping a stale choice.
class RideOptionsLoaded extends RideState {
  final List<RideOptionEstimate> options;
  final String selectedType;

  const RideOptionsLoaded({
    required this.options,
    required this.selectedType,
  });

  RideOptionEstimate? get selected {
    for (final o in options) {
      if (o.rideType == selectedType && o.available) return o;
    }
    return null;
  }

  @override
  List<Object?> get props => [options, selectedType];
}

/// §2.6: the estimate fetch failed. The carousel must render this as a
/// retryable "Prices unavailable" strip — never hardcoded cards or fake
/// price ranges.
class RideOptionsFailed extends RideState {
  final String message;

  const RideOptionsFailed(this.message);

  @override
  List<Object?> get props => [message];
}

/// Error state
class RideError extends RideState {
  final String message;
  final String? errorCode;

  const RideError({
    required this.message,
    this.errorCode,
  });

  @override
  List<Object?> get props => [message, errorCode];
}

// ============================================================================
// BLOC
// ============================================================================

class RideBloc extends Bloc<RideEvent, RideState> {
  final RideRepository _rideRepository;
  final RequestRideUseCase _requestRideUseCase;
  final GetRideStatusUseCase _getRideStatusUseCase;
  final SafetyRepository _safetyRepository;

  /// §2.3 role detection: resolves the signed-in account id so the bloc can
  /// tell whether this session is the ride's DRIVER (who must send the
  /// pre-selfie) or its RIDER (who verifies it). Null means "no session" —
  /// in that case we default to the rider view, which is the safe state:
  /// a rider never sees the driver's capture sheet, and an unauthenticated
  /// user has no actionable ride anyway.
  final String? Function()? _currentUserId;

  StreamSubscription<RideStatusUpdate>? _rideUpdateSubscription;
  Timer? _etaUpdateTimer;

  // --- SOS countdown state ---
  Timer? _sosCountdownTimer;
  int _sosCountdownRemaining = 0;
  bool _sosActive = false;

  /// How long the rider gets to cancel an accidental SOS hold.
  static const int sosCountdownSeconds = 5;

  /// Whether a safety flow (SOS / share) is currently running on this bloc.
  /// Kept separate from [state] so safety actions never clobber ride states.
  bool get isSosActive => _sosActive;
  int get sosCountdownRemaining => _sosCountdownRemaining;

  /// §2.6: the ride type the rider currently has selected in the carousel.
  /// Kept on the bloc so a refresh that drops the selected type can re-anchor
  /// the highlight to real data instead of leaving a stale choice that would
  /// be sent with RequestRide ("estimate ignores selected ride type" fix).
  String _currentSelectedRideType = 'standard';

  String get selectedRideType => _currentSelectedRideType;

  // --- §4.5 RideCheck anomaly detector state -------------------------------
  // The detector lives on the bloc so every consumer of ride state gets
  // identical behavior and the snooze/grace windows survive widget rebuilds.
  // All fields reset whenever the tracked ride changes or ClearCurrentRide
  // fires — stale samples from a previous ride must never trip a prompt on
  // a new one.

  /// Last driver position sample fed to the detector and its timestamp.
  LatLng? _lastFixPos;
  DateTime? _lastFixAt;

  /// When the vehicle first appeared stationary (within [rideCheckStopRadius]
  /// of the previous sample). Null while moving.
  DateTime? _stopSince;

  /// Reference point used for the "stopped away from pickup/dropoff" check:
  /// the last fix that was NOT near either endpoint, i.e. a stop in the
  /// corridor where a halt is suspicious.
  LatLng? _suspectStopPoint;

  /// While non-null the detector stays quiet (rider answered "I'm OK").
  DateTime? _rideCheckSnoozedUntil;

  /// The anomaly currently awaiting an answer, mirrored onto wrapped states
  /// as `pendingAnomaly`. Null when nothing is pending.
  RideAnomaly? _pendingAnomaly;

  /// Id of the ride the detector caches belong to; changing it resets them.
  String? _detectorRideId;

  /// A vehicle movement under this distance between successive samples
  /// counts as stopped (meters).
  static const double rideCheckStopRadius = 30.0;

  /// How long a stop outside pickup/dropoff proximity must persist before
  /// the "Is everything OK?" prompt fires (plan §4.5: >3 min).
  static const Duration rideCheckStopWindow = Duration(minutes: 3);

  /// Corridor deviation beyond which the route-deviation trigger fires
  /// (plan §4.5: >500 m off the expected polyline / straight-line reference).
  static const double rideCheckDeviationMeters = 500.0;

  /// Server ETA confidence below which the low-confidence trigger fires.
  static const double rideCheckEtaConfidenceFloor = 0.4;

  /// Grace period after the rider answers "I'm OK" before a new prompt can
  /// fire (prevents nagging while traffic genuinely crawls).
  static const Duration rideCheckSnoozeGrace = Duration(minutes: 2);

  /// Pickup/dropoff proximity radius inside which a stop is considered
  /// legitimate (traffic lights near endpoints, queues, etc.).
  static const double rideEndpointProximityMeters = 150.0;

  /// Cached context for the current ride, used to rebuild rich states when a
  /// WebSocket update arrives without a full Ride payload.
  double _currentFare = 0;
  DateTime? _requestedAt;
  DriverInfo? _lastKnownDriver;

  /// §2.3: authoritative `driver_id` from the last REST ride DTO, used to
  /// resolve the rider/driver role split for WS status payloads that omit
  /// it. Null until the first LoadRide succeeds — with null we default to
  /// the rider view (safe: no driver-only UI can fire without proof).
  String? _lastKnownDriverId;

  /// §2.3 last-known pre-selfie state. WS status payloads usually omit the
  /// selfie block (it is embedded on the REST ride DTO), so rich states are
  /// rebuilt from this cache rather than dropping to "unknown". A fresh REST
  /// value always wins; a null here means "never seen one for this ride",
  /// which the UI renders as honest unavailable — never a placeholder photo.
  PreSelfieInfo? _lastKnownPreSelfie;

  /// Deterministic per-ride pickup PIN (anti-impersonation). The driver app
  /// derives the same code from the ride id, so no server round-trip is
  /// needed to display it.
  static String pinCodeForRide(String rideId) {
    var hash = 0;
    for (final unit in rideId.codeUnits) {
      hash = (hash * 31 + unit) & 0x7FFFFFFF;
    }
    return (hash % 10000).toString().padLeft(4, '0');
  }

  /// Pending SOS details captured when the countdown starts, fired on expiry
  /// or on [ConfirmSos]. Null when no countdown is running.
  StartSosCountdown? _pendingSos;

  RideBloc({
    required RideRepository rideRepository,
    required RequestRideUseCase requestRideUseCase,
    required GetRideStatusUseCase getRideStatusUseCase,
    required SafetyRepository safetyRepository,
    String? Function()? currentUserIdResolver,
  })  : _rideRepository = rideRepository,
        _requestRideUseCase = requestRideUseCase,
        _getRideStatusUseCase = getRideStatusUseCase,
        _safetyRepository = safetyRepository,
        _currentUserId = currentUserIdResolver,
        super(const RideInitial()) {
    on<RequestRide>(_onRequestRide);
    on<LoadRide>(_onLoadRide);
    on<LoadRideHistory>(_onLoadRideHistory);
    on<CancelRide>(_onCancelRide);
    on<SubscribeToRideUpdates>(_onSubscribeToUpdates);
    on<UnsubscribeFromRideUpdates>(_onUnsubscribeFromUpdates);
    on<RateRide>(_onRateRide);
    on<GetFareEstimate>(_onGetFareEstimate);
    on<LoadRideOptions>(_onLoadRideOptions);
    on<SelectRideOption>(_onSelectRideOption);
    on<RideOptionSelectionChanged>(_onRideOptionSelectionChanged);
    on<ClearCurrentRide>(_onClearCurrentRide);
    on<StartSosCountdown>(_onStartSosCountdown);
    on<CancelSosCountdown>(_onCancelSosCountdown);
    on<ConfirmSos>(_onConfirmSos);
    on<ReportRidePosition>(_onReportRidePosition);
    on<DismissRideCheckPrompt>(_onDismissRideCheckPrompt);
  }

  // ==========================================================================
  // §4.5 RIDECHECK ANOMALY DETECTOR
  // ==========================================================================

  /// Clears every cached detector sample. Called when the tracked ride
  /// changes or the session is torn down — a stop observed on ride A must
  /// never age into a prompt on ride B.
  void _resetRideCheckDetector({String? forRideId}) {
    _lastFixPos = null;
    _lastFixAt = null;
    _stopSince = null;
    _suspectStopPoint = null;
    _rideCheckSnoozedUntil = null;
    _pendingAnomaly = null;
    _detectorRideId = forRideId;
  }

  /// Feeds one driver position sample into the detector and re-emits the
  /// current wrapped state if a new anomaly tripped (or cleared).
  Future<void> _onReportRidePosition(
    ReportRidePosition event,
    Emitter<RideState> emit,
  ) async {
    // Samples from a different ride than the one being tracked are ignored
    // outright; a ride-id change resets the caches first.
    if (_detectorRideId != null && _detectorRideId != event.rideId) {
      _resetRideCheckDetector(forRideId: event.rideId);
    } else if (_detectorRideId == null) {
      _detectorRideId = event.rideId;
    }

    final point = LatLng(event.lat, event.lng);
    final prev = _lastFixPos;
    final prevAt = _lastFixAt;
    _lastFixPos = point;
    _lastFixAt = event.at;

    if (prev == null || prevAt == null) return;

    final movedMeters = const Distance().as(
      LengthUnit.Meter,
      prev,
      point,
    );

    if (movedMeters >= rideCheckStopRadius) {
      // Vehicle moved — the stop window restarts. Remember whether this
      // fix sits in the suspicious corridor (away from both endpoints).
      _stopSince = null;
      _suspectStopPoint = _isNearEndpoint(point) ? null : point;
    } else if (_stopSince == null) {
      _stopSince = prevAt;
      if (!_isNearEndpoint(point)) _suspectStopPoint = point;
    }

    // With no authoritative endpoints the long-stop trigger must stay
    // dormant (dormant beats fabricated): drop any suspect marker so
    // Trigger 1 cannot fire on guesses.
    if (_endpointCandidates().isEmpty) {
      _suspectStopPoint = null;
    }

    // An unanswered prompt already pending — do not stack another.
    if (_pendingAnomaly != null) return;

    // Rider answered "I'm OK" recently — stay quiet during the grace window.
    final snooze = _rideCheckSnoozedUntil;
    if (snooze != null && event.at.isBefore(snooze)) return;

    RideAnomaly? anomaly;

    // Trigger 1: stopped >3 min outside pickup/dropoff proximity.
    final stopStart = _stopSince;
    if (stopStart != null &&
        _suspectStopPoint != null &&
        event.at.difference(stopStart) > rideCheckStopWindow) {
      anomaly = RideAnomaly(
        kind: RideAnomalyKind.longStop,
        detectedAt: event.at,
      );
    }

    // Trigger 2: deviation >500 m off the expected corridor. Reference
    // points come from real state (request DTO coords / GPS trip start);
    // with no reference the check stays dormant rather than assuming one.
    if (anomaly == null) {
      final refs = _corridorReferences();
      if (refs != null) {
        final deviation = _distanceToSegmentMeters(point, refs.$1, refs.$2);
        if (deviation > rideCheckDeviationMeters) {
          anomaly = RideAnomaly(
            kind: RideAnomalyKind.routeDeviation,
            detectedAt: event.at,
            detailMeters: deviation,
          );
        }
      }
    }

    // Trigger 3: server ETA confidence below threshold. Null confidence
    // means the backend did not publish reliability — never assume zero.
    if (anomaly == null &&
        event.etaConfidence != null &&
        event.etaConfidence! < rideCheckEtaConfidenceFloor) {
      anomaly = RideAnomaly(
        kind: RideAnomalyKind.lowEtaConfidence,
        detectedAt: event.at,
        confidence: event.etaConfidence,
      );
    }

    if (anomaly == null) return;
    _pendingAnomaly = anomaly;
    _reemitWithAnomaly(emit);
  }

  /// The rider answered the prompt. "I'm OK" snoozes the detector for the
  /// grace window and clears the pending anomaly; "Call for help" is routed
  /// by the UI to [StartSosCountdown] instead and never reaches here.
  Future<void> _onDismissRideCheckPrompt(
    DismissRideCheckPrompt event,
    Emitter<RideState> emit,
  ) async {
    if (event.ok) {
      _rideCheckSnoozedUntil = DateTime.now().add(rideCheckSnoozeGrace);
      // Reset the stop bookkeeping so the same halt cannot immediately
      // re-trip after the grace window without fresh evidence.
      _stopSince = null;
      _suspectStopPoint = null;
    }
    if (_pendingAnomaly == null) return;
    _pendingAnomaly = null;
    _reemitWithAnomaly(emit);
  }

  bool _isNearEndpoint(LatLng point) {
    final refs = _endpointCandidates();
    // No authoritative endpoints for this ride → the long-stop check stays
    // dormant rather than guessing; return false so no suspect stop is
    // recorded without real reference data.
    if (refs.isEmpty) return false;
    for (final e in refs) {
      if (const Distance().as(LengthUnit.Meter, point, e) <=
          rideEndpointProximityMeters) {
        return true;
      }
    }
    return false;
  }

  /// Pickup/dropoff endpoints from live state, if known. Empty list means
  /// the long-stop proximity check has no authoritative endpoints, in which
  /// case stops are NOT flagged (dormant beats fabricated).
  List<LatLng> _endpointCandidates() {
    final out = <LatLng>[];
    final s = state;
    double? pLat, pLng, dLat, dLng;
    String? rid;
    if (s is RideRequested) {
      rid = s.rideId;
      pLat = s.pickupLat;
      pLng = s.pickupLng;
      dLat = s.dropoffLat;
      dLng = s.dropoffLng;
    } else if (s is DriverMatched) {
      rid = s.rideId;
      pLat = s.pickupLat;
      pLng = s.pickupLng;
      dLat = s.dropoffLat;
      dLng = s.dropoffLng;
    } else if (s is DriverEnRoute) {
      rid = s.rideId;
      pLat = s.pickupLat;
      pLng = s.pickupLng;
      dLat = s.dropoffLat;
      dLng = s.dropoffLng;
    } else if (s is RideInProgress) {
      rid = s.ride.id;
      pLat = s.ride.pickupLat;
      pLng = s.ride.pickupLng;
      dLat = s.ride.dropoffLat;
      dLng = s.ride.dropoffLng;
    }
    if (rid != _detectorRideId) return out;
    if (pLat != null && pLng != null) out.add(LatLng(pLat, pLng));
    if (dLat != null && dLng != null) out.add(LatLng(dLat, dLng));
    return out;
  }

  /// Corridor reference (start, end) for the deviation check. En route:
  /// pickup→dropoff from state. In progress: the GPS trip-start origin
  /// (when captured) → dropoff. Any missing half returns null → dormant.
  (LatLng, LatLng)? _corridorReferences() {
    final s = state;
    if (s is DriverEnRoute &&
        s.pickupLat != null &&
        s.pickupLng != null &&
        s.dropoffLat != null &&
        s.dropoffLng != null &&
        s.rideId == _detectorRideId) {
      return (
        LatLng(s.pickupLat!, s.pickupLng!),
        LatLng(s.dropoffLat!, s.dropoffLng!),
      );
    }
    if (s is RideInProgress &&
        s.tripStartOrigin != null &&
        s.ride.dropoffLat != null &&
        s.ride.dropoffLng != null &&
        s.ride.id == _detectorRideId) {
      return (
        s.tripStartOrigin!,
        LatLng(s.ride.dropoffLat!, s.ride.dropoffLng!),
      );
    }
    return null;
  }

  /// Great-circle distance from [p] to the segment [a]→[b], meters. Uses
  /// the latlon projection approximation adequate at the 500 m trigger
  /// scale (the plan's polyline source is the straight-line corridor until
  /// the backend ships route geometry on the WS payload).
  double _distanceToSegmentMeters(LatLng p, LatLng a, LatLng b) {
    const dist = Distance();
    final ab = dist.as(LengthUnit.Meter, a, b);
    if (ab == 0) return dist.as(LengthUnit.Meter, p, a);
    // Project in a local equirectangular frame anchored at `a`.
    final latScale = 111320.0;
    final lngScale = 111320.0 * math.cos(p.latitude * math.pi / 180.0);
    final bx = (b.longitude - a.longitude) * lngScale;
    final by = (b.latitude - a.latitude) * latScale;
    final px = (p.longitude - a.longitude) * lngScale;
    final py = (p.latitude - a.latitude) * latScale;
    final len2 = bx * bx + by * by;
    var t = (px * bx + py * by) / len2;
    if (t < 0) t = 0;
    if (t > 1) t = 1;
    final cx = px - t * bx;
    final cy = py - t * by;
    return math.sqrt(cx * cx + cy * cy);
  }

  /// Re-emits the current wrapped state carrying [_pendingAnomaly] so the
  /// tracking page renders (or clears) the prompt without disturbing any
  /// other field. States that cannot host an anomaly are left untouched.
  void _reemitWithAnomaly(Emitter<RideState> emit) {
    final s = state;
    final anomaly = _pendingAnomaly;
    if (s is RideRequested) {
      emit(RideRequested(
        rideId: s.rideId,
        fare: s.fare,
        etaMinutes: s.etaMinutes,
        requestedAt: s.requestedAt,
        promoDiscount: s.promoDiscount,
        pickupLat: s.pickupLat,
        pickupLng: s.pickupLng,
        dropoffLat: s.dropoffLat,
        dropoffLng: s.dropoffLng,
        pickupName: s.pickupName,
        dropoffName: s.dropoffName,
        pendingAnomaly: anomaly,
      ));
    } else if (s is DriverMatched) {
      emit(DriverMatched(
        rideId: s.rideId,
        driver: s.driver,
        fare: s.fare,
        etaMinutes: s.etaMinutes,
        pickupLat: s.pickupLat,
        pickupLng: s.pickupLng,
        dropoffLat: s.dropoffLat,
        dropoffLng: s.dropoffLng,
        pickupName: s.pickupName,
        dropoffName: s.dropoffName,
        pendingAnomaly: anomaly,
      ));
    } else if (s is DriverEnRoute) {
      emit(DriverEnRoute(
        rideId: s.rideId,
        driver: s.driver,
        fare: s.fare,
        etaMinutes: s.etaMinutes,
        pinCode: s.pinCode,
        preSelfie: s.preSelfie,
        pickupLat: s.pickupLat,
        pickupLng: s.pickupLng,
        dropoffLat: s.dropoffLat,
        dropoffLng: s.dropoffLng,
        pickupName: s.pickupName,
        dropoffName: s.dropoffName,
        pendingAnomaly: anomaly,
      ));
    } else if (s is RideInProgress) {
      emit(RideInProgress(
        s.ride,
        tripStartOrigin: s.tripStartOrigin,
        pendingAnomaly: anomaly,
      ));
    }
  }

  // ==========================================================================
  // EVENT HANDLERS
  // ==========================================================================

  Future<void> _onRequestRide(
    RequestRide event,
    Emitter<RideState> emit,
  ) async {
    emit(const RideLoading());

    try {
      final result = await _requestRideUseCase.execute(
        pickupLat: event.pickupLat,
        pickupLng: event.pickupLng,
        dropoffLat: event.dropoffLat,
        dropoffLng: event.dropoffLng,
        pickupAddress: event.pickupAddress,
        dropoffAddress: event.dropoffAddress,
        rideType: event.rideType,
        paymentMethodId: event.paymentMethodId,
        paymentMethodType: event.paymentMethodType,
        promoCode: event.promoCode,
        scheduledAt: event.scheduledAt,
        stops: event.stops,
      );

      if (result.isFailure) {
        // Payment declines are recoverable — surface a dedicated state so the
        // UI can offer retry / switch-rail / top-up instead of a dead end.
        if (result.errorCode == 'PAYMENT_FAILED' ||
            result.errorCode == 'INVALID_PAYMENT') {
          emit(PaymentFailed(
            message: result.error ?? 'Payment could not be processed',
            errorCode: result.errorCode,
          ));
          return;
        }
        emit(RideError(
          message: result.error ?? 'Failed to request ride',
          errorCode: result.errorCode,
        ));
        return;
      }

      _currentFare = result.fare ?? 0;
      _requestedAt = DateTime.now();
      _lastKnownDriver = null;
      _lastKnownPreSelfie = null;

      emit(RideRequested(
        rideId: result.rideId!,
        fare: result.fare!,
        etaMinutes: result.eta!,
        requestedAt: DateTime.now(),
        promoDiscount: result.promoDiscount,
        // §4.1: carry the rider's own request coordinates into the state so
        // tracking can render the pickup pin and the AR entry immediately,
        // before the first REST poll returns the full ride record.
        pickupLat: event.pickupLat,
        pickupLng: event.pickupLng,
        dropoffLat: event.dropoffLat,
        dropoffLng: event.dropoffLng,
        pickupName: event.pickupAddress,
        dropoffName: event.dropoffAddress,
      ));

      // Automatically subscribe to updates
      add(SubscribeToRideUpdates(result.rideId!));
    } on ApiException catch (e) {
      if (e.errorCode == 'PAYMENT_FAILED' ||
          e.errorCode == 'INVALID_PAYMENT') {
        emit(PaymentFailed(
          message: e.message,
          errorCode: e.errorCode,
        ));
        return;
      }
      emit(RideError(
        message: e.message,
        errorCode: e.errorCode ?? 'REQUEST_FAILED',
      ));
    } catch (e) {
      emit(RideError(
        message: e.toString(),
        errorCode: 'REQUEST_FAILED',
      ));
    }
  }

  Future<void> _onLoadRide(
    LoadRide event,
    Emitter<RideState> emit,
  ) async {
    emit(const RideLoading());

    try {
      final ride = await _rideRepository.getRide(event.rideId);
      if (ride == null) {
        emit(const RideError(
          message: 'Ride not found',
          errorCode: 'NOT_FOUND',
        ));
        return;
      }

      _emitStateForRide(emit, ride);
    } catch (e) {
      emit(RideError(
        message: e.toString(),
        errorCode: 'LOAD_FAILED',
      ));
    }
  }

  Future<void> _onLoadRideHistory(
    LoadRideHistory event,
    Emitter<RideState> emit,
  ) async {
    emit(const RideLoading());

    try {
      final result = await _rideRepository.listRides(
        status: event.status,
        limit: event.limit,
        offset: event.offset,
      );

      emit(RideHistoryLoaded(
        rides: result.rides,
        total: result.total,
        hasMore: result.hasMore,
      ));
    } catch (e) {
      emit(RideError(
        message: e.toString(),
        errorCode: 'LOAD_HISTORY_FAILED',
      ));
    }
  }

  Future<void> _onCancelRide(
    CancelRide event,
    Emitter<RideState> emit,
  ) async {
    emit(const RideLoading());

    try {
      final result = await _rideRepository.cancelRideWithResult(
        event.rideId,
        reason: event.reason,
      );

      if (!result.success) {
        // The ride may already be gone (e.g. driver cancelled first) — in
        // that case land the user in the cancelled state, not an error.
        if (result.wasDriverCancelled || result.errorCode == 'INVALID_TRANSITION') {
          add(UnsubscribeFromRideUpdates(event.rideId));
          emit(RideCancelled(
            rideId: event.rideId,
            reason: result.message,
            cancelledBy: 'driver',
          ));
          return;
        }
        emit(RideError(
          message: result.message ?? 'Failed to cancel ride',
          errorCode: result.errorCode ?? 'CANCEL_FAILED',
        ));
        return;
      }

      add(UnsubscribeFromRideUpdates(event.rideId));
      emit(RideCancelled(
        rideId: event.rideId,
        reason: event.reason ?? 'Cancelled by user',
        cancelledBy: 'rider',
        cancellationFee: result.cancellationFee,
        currency: result.currency,
      ));
    } on ApiException catch (e) {
      emit(RideError(
        message: e.message,
        errorCode: e.errorCode ?? 'CANCEL_FAILED',
      ));
    } catch (e) {
      emit(RideError(
        message: e.toString(),
        errorCode: 'CANCEL_FAILED',
      ));
    }
  }

  Future<void> _onSubscribeToUpdates(
    SubscribeToRideUpdates event,
    Emitter<RideState> emit,
  ) async {
    // Cancel existing subscription
    await _rideUpdateSubscription?.cancel();

    _rideUpdateSubscription = _rideRepository
        .subscribeToRideUpdates(event.rideId)
        .listen((update) {
      _handleRideUpdate(update, emit);
    });

    // Start ETA update timer
    _startETATimer(event.rideId, emit);
  }

  Future<void> _onUnsubscribeFromUpdates(
    UnsubscribeFromRideUpdates event,
    Emitter<RideState> emit,
  ) async {
    await _rideUpdateSubscription?.cancel();
    _rideUpdateSubscription = null;
    _etaUpdateTimer?.cancel();
    _etaUpdateTimer = null;

    _rideRepository.unsubscribeFromRideUpdates(event.rideId);
  }

  Future<void> _onRateRide(
    RateRide event,
    Emitter<RideState> emit,
  ) async {
    try {
      final result = await _rideRepository.rateRideWithResult(
        event.rideId,
        event.rating,
        review: event.review,
        tip: event.tip,
      );

      if (!result.success) {
        // Already-rated is a benign double-submit — treat as success so the
        // rating sheet can close instead of trapping the user on an error.
        if (result.alreadyRated) {
          emit(RideRated(
            rideId: event.rideId,
            rating: event.rating,
            tip: event.tip ?? 0,
          ));
          return;
        }
        emit(RideError(
          message: result.message ?? 'Failed to rate ride',
          errorCode: result.errorCode ?? 'RATE_FAILED',
        ));
        return;
      }

      emit(RideRated(
        rideId: event.rideId,
        rating: event.rating,
        tip: event.tip ?? 0,
      ));

      // Reload ride to get updated state
      add(LoadRide(event.rideId));
    } on ApiException catch (e) {
      emit(RideError(
        message: e.message,
        errorCode: e.errorCode ?? 'RATE_FAILED',
      ));
    } catch (e) {
      emit(RideError(
        message: e.toString(),
        errorCode: 'RATE_FAILED',
      ));
    }
  }

  Future<void> _onGetFareEstimate(
    GetFareEstimate event,
    Emitter<RideState> emit,
  ) async {
    emit(const RideLoading());

    try {
      final estimate = await _rideRepository.getFareEstimate(
        pickupLat: event.pickupLat,
        pickupLng: event.pickupLng,
        dropoffLat: event.dropoffLat,
        dropoffLng: event.dropoffLng,
        rideType: event.rideType,
      );

      if (estimate == null) {
        emit(const RideError(
          message: 'Failed to get fare estimate',
          errorCode: 'ESTIMATE_FAILED',
        ));
        return;
      }

      emit(FareEstimateLoaded(estimate));
    } catch (e) {
      emit(RideError(
        message: e.toString(),
        errorCode: 'ESTIMATE_FAILED',
      ));
    }
  }

  // ==========================================================================
  // §2.6 RIDE-OPTION CAROUSEL
  // ==========================================================================

  Future<void> _onLoadRideOptions(
    LoadRideOptions event,
    Emitter<RideState> emit,
  ) async {
    try {
      final options = await _rideRepository.getRideOptionEstimates(
        pickupLat: event.pickupLat,
        pickupLng: event.pickupLng,
        dropoffLat: event.dropoffLat,
        dropoffLng: event.dropoffLng,
      );

      if (options.isEmpty) {
        emit(const RideOptionsFailed('No ride options are currently available'));
        return;
      }

      _currentSelectedRideType = event.selectedType;

      // Re-anchor honestly: if the refreshed list no longer contains the
      // selected type (or marks it unavailable), fall back to the first
      // available option and tell the page so its highlight follows the data.
      final stillValid = options.any(
          (o) => o.rideType == event.selectedType && o.available);
      if (!stillValid) {
        final fallback =
            options.firstWhere((o) => o.available, orElse: () => options.first);
        _currentSelectedRideType = fallback.rideType;
        add(RideOptionSelectionChanged(fallback.rideType));
      }

      emit(RideOptionsLoaded(
        options: options,
        selectedType: _currentSelectedRideType,
      ));
    } catch (e) {
      emit(RideOptionsFailed(e.toString()));
    }
  }

  void _onSelectRideOption(
    SelectRideOption event,
    Emitter<RideState> emit,
  ) {
    _currentSelectedRideType = event.rideType;
    final currentState = state;
    if (currentState is RideOptionsLoaded) {
      emit(RideOptionsLoaded(
        options: currentState.options,
        selectedType: event.rideType,
      ));
    }
  }

  void _onRideOptionSelectionChanged(
    RideOptionSelectionChanged event,
    Emitter<RideState> emit,
  ) {
    _currentSelectedRideType = event.rideType;
    final currentState = state;
    if (currentState is RideOptionsLoaded) {
      emit(RideOptionsLoaded(
        options: currentState.options,
        selectedType: event.rideType,
      ));
    }
  }

  void _onClearCurrentRide(
    ClearCurrentRide event,
    Emitter<RideState> emit,
  ) {
    _sosCountdownTimer?.cancel();
    _sosCountdownTimer = null;
    _pendingSos = null;
    _sosActive = false;
    _sosCountdownRemaining = 0;
    // §4.5: drop every detector sample so a stop observed on the previous
    // ride can never age into a prompt on the next one.
    _resetRideCheckDetector();
    emit(const RideInitial());
  }

  // ==========================================================================
  // SAFETY / SOS HANDLERS
  // ==========================================================================

  /// Starts the pre-alarm countdown. The UI polls [sosCountdownRemaining]
  /// (updated once per second) while [isSosActive] is true; when the timer
  /// expires the SOS fires automatically unless cancelled.
  void _onStartSosCountdown(
    StartSosCountdown event,
    Emitter<RideState> emit,
  ) {
    // Ignore re-entrant starts while a countdown or dispatch is in flight.
    if (_sosActive) return;

    _pendingSos = event;
    _sosActive = true;
    _sosCountdownRemaining = sosCountdownSeconds;

    _sosCountdownTimer?.cancel();
    _sosCountdownTimer = Timer.periodic(
      const Duration(seconds: 1),
      (timer) async {
        _sosCountdownRemaining -= 1;
        if (_sosCountdownRemaining <= 0) {
          timer.cancel();
          await _dispatchSos(emit);
        }
      },
    );
  }

  /// Aborts an in-flight countdown (false alarm). If the SOS has already been
  /// dispatched this is a no-op — disqualification happens server-side.
  void _onCancelSosCountdown(
    CancelSosCountdown event,
    Emitter<RideState> emit,
  ) {
    _sosCountdownTimer?.cancel();
    _sosCountdownTimer = null;
    _pendingSos = null;
    _sosActive = false;
    _sosCountdownRemaining = 0;
  }

  /// Skips the remainder of the countdown and fires immediately.
  Future<void> _onConfirmSos(
    ConfirmSos event,
    Emitter<RideState> emit,
  ) async {
    if (_pendingSos == null) return;
    _sosCountdownTimer?.cancel();
    _sosCountdownTimer = null;
    await _dispatchSos(emit);
  }

  /// Posts the SOS to the safety backend. Deliberately does NOT emit ride
  /// states — safety runs on side-channel getters so it can never clobber
  /// the active ride state (tracking map, ETA card, etc.).
  Future<void> _dispatchSos(Emitter<RideState> emit) async {
    final pending = _pendingSos;
    _sosCountdownTimer?.cancel();
    _sosCountdownTimer = null;
    _pendingSos = null;
    _sosCountdownRemaining = 0;

    if (pending == null) {
      _sosActive = false;
      return;
    }

    try {
      final result = await _safetyRepository.triggerSos(
        rideId: pending.rideId,
        lat: pending.lat,
        lng: pending.lng,
        message: pending.message,
      );
      // On failure the flow stays "active" so the UI can show retry options;
      // on success the incident id is retrievable via the repository layer.
      _sosActive = !result.success;
      if (!result.success) {
        debugPrint('SOS dispatch failed: ${result.errorCode} ${result.message}');
      }
    } catch (e) {
      _sosActive = false;
      debugPrint('SOS dispatch threw: $e');
    }
  }

  // ==========================================================================
  // HELPERS
  // ==========================================================================

  void _handleRideUpdate(RideStatusUpdate update, Emitter<RideState> emit) {
    // §4.5 RideCheck: feed every driver position sample into the anomaly
    // detector before state transitions, so stop/deviation windows see a
    // monotonic stream regardless of which state the update lands in. The
    // detector is a no-op for payloads without coordinates.
    if (update.driverLat != null && update.driverLng != null) {
      add(ReportRidePosition(
        rideId: update.rideId,
        lat: update.driverLat!,
        lng: update.driverLng!,
        at: DateTime.now(),
        etaConfidence: update.etaConfidence ?? update.driver?.etaConfidence,
      ));
    }
    // §4.1: the WS status payload omits the ride's pickup/dropoff coordinates
    // (they live on the REST DTO / request event). When a searching→matched
    // transition happens over WS, re-emit them onto the new state so the
    // tracking map keeps its pickup pin and the AR entry stays wired through
    // the whole pre-match lifecycle. Values are copied from the live
    // RideRequested state — never fabricated.
    RideRequested? _carryCoords(RideRequested? prev) => prev;
    final prevRequested =
        state is RideRequested ? state as RideRequested : null;
    switch (update.status) {
      case 'requested':
      case 'searching':
        // Stay in requested state
        break;
      case 'matched':
        if (update.driver != null) {
          _lastKnownDriver = update.driver;
          final fare = _stateFare();
          emit(DriverMatched(
            rideId: update.rideId,
            driver: update.driver!,
            fare: fare,
            etaMinutes: update.driver!.etaMinutes,
            // §4.1: real pickup coords carried from the live request state so
            // the AR walk-to-pickup entry has authoritative data from the
            // moment of match (null when unavailable → AR button hidden,
            // never shown with fake coordinates).
            pickupLat: _carryCoords(prevRequested)?.pickupLat,
            pickupLng: _carryCoords(prevRequested)?.pickupLng,
            dropoffLat: _carryCoords(prevRequested)?.dropoffLat,
            dropoffLng: _carryCoords(prevRequested)?.dropoffLng,
            pickupName: _carryCoords(prevRequested)?.pickupName,
            dropoffName: _carryCoords(prevRequested)?.dropoffName,
          ));
        }
        break;
      case 'driver_en_route':
        final driver = update.driver ?? _lastKnownDriver;
        // §2.3 role split (mirrors _emitStateForRide): if this session is
        // the ride's driver, emit the driver-side state so the tracking
        // page can fire the pre-selfie capture sheet instead of the
        // rider-only verification card.
        final me = _currentUserId?.call();
        final iAmDriver = me != null &&
            _lastKnownDriverId != null &&
            _lastKnownDriverId == me;
        if (iAmDriver) {
          emit(DriverEnRouteForDriver(
            rideId: update.rideId,
            fare: _stateFare(),
            etaMinutes: driver?.etaMinutes ?? 0,
          ));
        } else if (driver != null) {
          _lastKnownDriver = driver;
          emit(DriverEnRoute(
            rideId: update.rideId,
            driver: driver,
            fare: _stateFare(),
            etaMinutes: driver.etaMinutes,
            pinCode: pinCodeForRide(update.rideId),
            preSelfie: update.preSelfie ?? _lastKnownPreSelfie,
            // §4.1: carried from the live request state — never fabricated.
            pickupLat: _carryCoords(prevRequested)?.pickupLat,
            pickupLng: _carryCoords(prevRequested)?.pickupLng,
            dropoffLat: _carryCoords(prevRequested)?.dropoffLat,
            dropoffLng: _carryCoords(prevRequested)?.dropoffLng,
            pickupName: _carryCoords(prevRequested)?.pickupName,
            dropoffName: _carryCoords(prevRequested)?.dropoffName,
          ));
        } else if (update.driver != null) {
          // Defensive: never drop a payload that does carry a driver.
          _lastKnownDriver = update.driver;
        }
        break;
      case 'no_driver_available':
        add(UnsubscribeFromRideUpdates(update.rideId));
        // §4.6: carry the last-known broadcast fare so the price-extension
        // sheet can show real +10%/+20% projections. _stateFareOrNull keeps
        // it null when no fare was ever learned — the sheet then defers every
        // number to the server instead of inventing one (Global Rule 1).
        emit(NoDriversFound(
          rideId: update.rideId,
          searchedSeconds: _searchedSeconds(),
          currentFare: _stateFareOrNull(),
          currency: _stateCurrency(),
        ));
        break;
      case 'in_progress':
        // Load full ride details
        add(LoadRide(update.rideId));
        break;
      case 'completed':
        add(LoadRide(update.rideId));
        add(UnsubscribeFromRideUpdates(update.rideId));
        break;
      case 'cancelled':
        final cancelledBy = update.cancelledBy ?? 'system';
        emit(RideCancelled(
          rideId: update.rideId,
          reason: update.cancelReason ??
              (cancelledBy == 'driver'
                  ? 'Your driver cancelled the ride'
                  : 'Ride cancelled'),
          cancelledBy: cancelledBy,
          // Rider-initiated cancellations surface their fee via the
          // CancelRide command result; remote cancels are informational.
          cancellationFee: _cancellationFeeFromState(),
        ));
        add(UnsubscribeFromRideUpdates(update.rideId));
        break;
    }
  }

  /// Best-known fare for the current ride from live state or cache.
  double _stateFare() {
    final currentState = state;
    if (currentState is RideRequested) return currentState.fare;
    if (currentState is DriverMatched) return currentState.fare;
    if (currentState is DriverEnRoute) return currentState.fare;
    return _currentFare;
  }

  /// §4.6: nullable sibling of [_stateFare] for surfaces that must render an
  /// honest "fare unavailable" instead of a fabricated ETB 0.00 — returns
  /// null when no fare was ever learned for this ride (fresh bloc, WS fired
  /// before REST hydration), otherwise the best-known positive fare.
  double? _stateFareOrNull() {
    final f = _stateFare();
    return f > 0 ? f : null;
  }

  /// §4.6: ISO-4217 code paired with the best-known fare. Taken from the
  /// authoritative REST DTO when hydrated ([_lastKnownCurrency]); null until
  /// then so the UI renders an empty unit rather than guessing 'ETB'.
  String? _stateCurrency() => _lastKnownCurrency;

  String? _lastKnownCurrency;

  /// Seconds elapsed since the request was placed (for No-Drivers messaging).
  int _searchedSeconds() {
    final started = _requestedAt;
    if (started == null) return 0;
    return DateTime.now().difference(started).inSeconds;
  }

  double _cancellationFeeFromState() {
    final currentState = state;
    if (currentState is RideCancelled) return currentState.cancellationFee;
    return 0;
  }

  void _emitStateForRide(Emitter<RideState> emit, Ride ride) {
    _currentFare = ride.fareAmount ?? 0;
    // §4.6: the REST DTO is the only authoritative currency source; cache it
    // so NoDriversFound (WS or REST path) pairs fares with a real unit and
    // never guesses 'ETB'/'USD'.
    _lastKnownCurrency = ride.currency;
    _requestedAt = ride.requestedAt;
    if (ride.driver != null) _lastKnownDriver = ride.driver;
    // REST DTO is the authoritative source for §2.3 pre-selfie state; a
    // missing block means the feature isn't serving this ride (flag off),
    // which we normalize to the honest `absent` (unavailable) info rather
    // than leaving stale data from a previous poll.
    _lastKnownPreSelfie = ride.preSelfie ?? PreSelfieInfo.absent;
    // §2.3: remember the authoritative driver id from the REST DTO so WS
    // status payloads (which omit it) can still resolve the rider/driver
    // role split below.
    _lastKnownDriverId = ride.driverId;

    switch (ride.status) {
      case 'requested':
      case 'searching':
        emit(RideRequested(
          rideId: ride.id,
          fare: ride.fareAmount ?? 0,
          etaMinutes: ride.durationMinutes ?? 0,
          requestedAt: ride.requestedAt,
          // §4.1: the REST DTO is authoritative for coordinates on reload —
          // a cold re-entry into tracking keeps the pickup pin and AR entry
          // working without waiting for a live request event.
          pickupLat: ride.pickupLat,
          pickupLng: ride.pickupLng,
          dropoffLat: ride.dropoffLat,
          dropoffLng: ride.dropoffLng,
          pickupName: ride.pickupAddress,
          dropoffName: ride.dropoffAddress,
        ));
        break;
      case 'matched':
        if (ride.driver != null) {
          emit(DriverMatched(
            rideId: ride.id,
            driver: ride.driver!,
            fare: ride.fareAmount ?? 0,
            etaMinutes: ride.driver!.etaMinutes,
            // §4.1: REST DTO is authoritative on reload — cold re-entry into
            // tracking keeps the pickup pin + AR entry without a live request.
            pickupLat: ride.pickupLat,
            pickupLng: ride.pickupLng,
            dropoffLat: ride.dropoffLat,
            dropoffLng: ride.dropoffLng,
            pickupName: ride.pickupAddress,
            dropoffName: ride.dropoffAddress,
          ));
        }
        break;
      case 'driver_en_route':
        // §2.3 role split: the signed-in account that owns ride.driverId is
        // the DRIVER — it gets DriverEnRouteForDriver (no rider-side selfie
        // card; instead the tracking page fires the capture sheet). Everyone
        // else in this session view is the RIDER and gets the full
        // DriverEnRoute state with the pre-selfie verification block.
        final me = _currentUserId?.call();
        if (me != null && ride.driverId != null && ride.driverId == me) {
          emit(DriverEnRouteForDriver(
            rideId: ride.id,
            fare: ride.fareAmount ?? 0,
            etaMinutes: ride.driver?.etaMinutes ?? 0,
          ));
        } else if (ride.driver != null) {
          emit(DriverEnRoute(
            rideId: ride.id,
            driver: ride.driver!,
            fare: ride.fareAmount ?? 0,
            etaMinutes: ride.driver!.etaMinutes,
            pinCode: pinCodeForRide(ride.id),
            preSelfie: _lastKnownPreSelfie,
            // §4.1: REST DTO is authoritative on reload — cold re-entry into
            // tracking keeps the pickup pin + AR entry without a live request.
            pickupLat: ride.pickupLat,
            pickupLng: ride.pickupLng,
            dropoffLat: ride.dropoffLat,
            dropoffLng: ride.dropoffLng,
            pickupName: ride.pickupAddress,
            dropoffName: ride.dropoffAddress,
          ));
        }
        break;
      case 'no_driver_available':
        // §4.6: same honest fare/currency carry-over as the WS path — the
        // REST DTO is authoritative, and a null fare stays null rather than
        // collapsing to 0 (which the sheet would render as "ETB 0.00").
        emit(NoDriversFound(
          rideId: ride.id,
          searchedSeconds: _searchedSeconds(),
          currentFare: _stateFareOrNull(),
          currency: ride.currency,
        ));
        break;
      case 'in_progress':
        emit(RideInProgress(ride));
        break;
      case 'completed':
        emit(RideCompleted(ride));
        break;
      case 'cancelled':
        emit(RideCancelled(
          rideId: ride.id,
          reason: 'Ride cancelled',
        ));
        break;
    }
  }

  void _startETATimer(String rideId, Emitter<RideState> emit) {
    _etaUpdateTimer?.cancel();
    _etaUpdateTimer = Timer.periodic(const Duration(seconds: 30), (_) {
      // Reload ride to get updated ETA
      add(LoadRide(rideId));
    });
  }

  // ==========================================================================
  // CLEANUP
  // ==========================================================================

  @override
  Future<void> close() async {
    await _rideUpdateSubscription?.cancel();
    _etaUpdateTimer?.cancel();
    _sosCountdownTimer?.cancel();
    return super.close();
  }
}