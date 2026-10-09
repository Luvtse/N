import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:equatable/equatable.dart';

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

  const RequestRide({
    required this.pickupLat,
    required this.pickupLng,
    required this.dropoffLat,
    required this.dropoffLng,
    this.pickupAddress,
    this.dropoffAddress,
    this.rideType = 'standard',
    this.paymentMethodId,
  });

  @override
  List<Object?> get props => [
        pickupLat, pickupLng, dropoffLat, dropoffLng,
        pickupAddress, dropoffAddress, rideType, paymentMethodId,
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

  const GetFareEstimate({
    required this.pickupLat,
    required this.pickupLng,
    required this.dropoffLat,
    required this.dropoffLng,
  });

  @override
  List<Object?> get props => [pickupLat, pickupLng, dropoffLat, dropoffLng];
}

/// Clear current ride
class ClearCurrentRide extends RideEvent {
  const ClearCurrentRide();
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

  const RideRequested({
    required this.rideId,
    required this.fare,
    required this.etaMinutes,
    required this.requestedAt,
  });

  @override
  List<Object?> get props => [rideId, fare, etaMinutes, requestedAt];
}

/// Driver matched
class DriverMatched extends RideState {
  final String rideId;
  final DriverInfo driver;
  final double fare;
  final int etaMinutes;

  const DriverMatched({
    required this.rideId,
    required this.driver,
    required this.fare,
    required this.etaMinutes,
  });

  @override
  List<Object?> get props => [rideId, driver, fare, etaMinutes];
}

/// Ride in progress
class RideInProgress extends RideState {
  final Ride ride;

  const RideInProgress(this.ride);

  @override
  List<Object?> get props => [ride];
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

/// No drivers found for the request — offer recovery actions.
class NoDriversFound extends RideState {
  final String rideId;
  final int searchedSeconds;

  const NoDriversFound({
    required this.rideId,
    this.searchedSeconds = 0,
  });

  @override
  List<Object?> get props => [rideId, searchedSeconds];
}

/// Driver is en route to pickup — dedicated arrival state.
class DriverEnRoute extends RideState {
  final String rideId;
  final DriverInfo driver;
  final double fare;
  final int etaMinutes;

  /// PIN the rider shows the driver to start the trip (anti-impersonation).
  final String pinCode;

  const DriverEnRoute({
    required this.rideId,
    required this.driver,
    required this.fare,
    required this.etaMinutes,
    required this.pinCode,
  });

  @override
  List<Object?> get props => [rideId, driver, fare, etaMinutes, pinCode];
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

  /// Cached context for the current ride, used to rebuild rich states when a
  /// WebSocket update arrives without a full Ride payload.
  double _currentFare = 0;
  DateTime? _requestedAt;
  DriverInfo? _lastKnownDriver;

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

  RideBloc({
    required RideRepository rideRepository,
    required RequestRideUseCase requestRideUseCase,
    required GetRideStatusUseCase getRideStatusUseCase,
    SafetyRepository? safetyRepository,
  })  : _rideRepository = rideRepository,
        _requestRideUseCase = requestRideUseCase,
        _getRideStatusUseCase = getRideStatusUseCase,
        _safetyRepository =
            safetyRepository ?? NoopSafetyRepository(),
        super(const RideInitial()) {
    on<RequestRide>(_onRequestRide);
    on<LoadRide>(_onLoadRide);
    on<LoadRideHistory>(_onLoadRideHistory);
    on<CancelRide>(_onCancelRide);
    on<SubscribeToRideUpdates>(_onSubscribeToUpdates);
    on<UnsubscribeFromRideUpdates>(_onUnsubscribeFromUpdates);
    on<RateRide>(_onRateRide);
    on<GetFareEstimate>(_onGetFareEstimate);
    on<ClearCurrentRide>(_onClearCurrentRide);
    on<StartSosCountdown>(_onStartSosCountdown);
    on<CancelSosCountdown>(_onCancelSosCountdown);
    on<ConfirmSos>(_onConfirmSos);
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

      emit(RideRequested(
        rideId: result.rideId!,
        fare: result.fare!,
        etaMinutes: result.eta!,
        requestedAt: DateTime.now(),
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

  void _onClearCurrentRide(
    ClearCurrentRide event,
    Emitter<RideState> emit,
  ) {
    emit(const RideInitial());
  }

  // ==========================================================================
  // HELPERS
  // ==========================================================================

  void _handleRideUpdate(RideStatusUpdate update, Emitter<RideState> emit) {
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
          ));
        }
        break;
      case 'driver_en_route':
        final driver = update.driver ?? _lastKnownDriver;
        if (driver != null) {
          _lastKnownDriver = driver;
          emit(DriverEnRoute(
            rideId: update.rideId,
            driver: driver,
            fare: _stateFare(),
            etaMinutes: driver.etaMinutes,
            pinCode: pinCodeForRide(update.rideId),
          ));
        } else if (update.driver != null) {
          // Defensive: never drop a payload that does carry a driver.
          _lastKnownDriver = update.driver;
        }
        break;
      case 'no_driver_available':
        add(UnsubscribeFromRideUpdates(update.rideId));
        emit(NoDriversFound(
          rideId: update.rideId,
          searchedSeconds: _searchedSeconds(),
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
    _requestedAt = ride.requestedAt;
    if (ride.driver != null) _lastKnownDriver = ride.driver;

    switch (ride.status) {
      case 'requested':
      case 'searching':
        emit(RideRequested(
          rideId: ride.id,
          fare: ride.fareAmount ?? 0,
          etaMinutes: ride.durationMinutes ?? 0,
          requestedAt: ride.requestedAt,
        ));
        break;
      case 'matched':
        if (ride.driver != null) {
          emit(DriverMatched(
            rideId: ride.id,
            driver: ride.driver!,
            fare: ride.fareAmount ?? 0,
            etaMinutes: ride.driver!.etaMinutes,
          ));
        }
        break;
      case 'driver_en_route':
        if (ride.driver != null) {
          emit(DriverEnRoute(
            rideId: ride.id,
            driver: ride.driver!,
            fare: ride.fareAmount ?? 0,
            etaMinutes: ride.driver!.etaMinutes,
            pinCode: pinCodeForRide(ride.id),
          ));
        }
        break;
      case 'no_driver_available':
        emit(NoDriversFound(
          rideId: ride.id,
          searchedSeconds: _searchedSeconds(),
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
    return super.close();
  }
}