import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:equatable/equatable.dart';

import '../../data/repositories/ride_repository.dart';
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

  const RideCancelled({
    required this.rideId,
    required this.reason,
  });

  @override
  List<Object?> get props => [rideId, reason];
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

  StreamSubscription<RideStatusUpdate>? _rideUpdateSubscription;
  Timer? _etaUpdateTimer;

  RideBloc({
    required RideRepository rideRepository,
    required RequestRideUseCase requestRideUseCase,
    required GetRideStatusUseCase getRideStatusUseCase,
  })  : _rideRepository = rideRepository,
        _requestRideUseCase = requestRideUseCase,
        _getRideStatusUseCase = getRideStatusUseCase,
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
        emit(RideError(
          message: result.error ?? 'Failed to request ride',
          errorCode: result.errorCode,
        ));
        return;
      }

      emit(RideRequested(
        rideId: result.rideId!,
        fare: result.fare!,
        etaMinutes: result.eta!,
        requestedAt: DateTime.now(),
      ));

      // Automatically subscribe to updates
      add(SubscribeToRideUpdates(result.rideId!));
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
      final success = await _rideRepository.cancelRide(
        event.rideId,
        reason: event.reason,
      );

      if (!success) {
        emit(const RideError(
          message: 'Failed to cancel ride',
          errorCode: 'CANCEL_FAILED',
        ));
        return;
      }

      add(UnsubscribeFromRideUpdates(event.rideId));
      emit(RideCancelled(
        rideId: event.rideId,
        reason: event.reason ?? 'Cancelled by user',
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
      final success = await _rideRepository.rateRide(
        event.rideId,
        event.rating,
        review: event.review,
        tip: event.tip,
      );

      if (!success) {
        emit(const RideError(
          message: 'Failed to rate ride',
          errorCode: 'RATE_FAILED',
        ));
        return;
      }

      // Reload ride to get updated state
      add(LoadRide(event.rideId));
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
      case 'driver_en_route':
        if (update.driver != null) {
          final currentState = state;
          if (currentState is RideRequested) {
            emit(DriverMatched(
              rideId: update.rideId,
              driver: update.driver!,
              fare: currentState.fare,
              etaMinutes: update.driver!.etaMinutes,
            ));
          }
        }
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
        emit(RideCancelled(
          rideId: update.rideId,
          reason: 'Ride cancelled',
        ));
        add(UnsubscribeFromRideUpdates(update.rideId));
        break;
    }
  }

  void _emitStateForRide(Emitter<RideState> emit, Ride ride) {
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
      case 'driver_en_route':
        if (ride.driver != null) {
          emit(DriverMatched(
            rideId: ride.id,
            driver: ride.driver!,
            fare: ride.fareAmount ?? 0,
            etaMinutes: ride.driver!.etaMinutes,
          ));
        }
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