import 'dart:async';

import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:equatable/equatable.dart';

import '../../../../core/network/location_service.dart';
import '../../data/repositories/ride_repository.dart';
import '../../domain/entities/active_ride.dart';
import '../../domain/usecases/accept_ride_usecase.dart';
import '../../domain/usecases/start_ride_usecase.dart';
import '../../domain/usecases/complete_ride_usecase.dart';

// Events
abstract class ActiveRideEvent extends Equatable {
  const ActiveRideEvent();

  @override
  List<Object?> get props => [];
}

class AcceptRideRequested extends ActiveRideEvent {
  final String rideId;

  const AcceptRideRequested({required this.rideId});

  @override
  List<Object?> get props => [rideId];
}

class StartRideRequested extends ActiveRideEvent {
  final String rideId;

  const StartRideRequested({required this.rideId});

  @override
  List<Object?> get props => [rideId];
}

class CompleteRideRequested extends ActiveRideEvent {
  final String rideId;
  final double tipAmount;

  const CompleteRideRequested({
    required this.rideId,
    required this.tipAmount,
  });

  @override
  List<Object?> get props => [rideId, tipAmount];
}

class RideUpdated extends ActiveRideEvent {
  final ActiveRide ride;

  const RideUpdated({required this.ride});

  @override
  List<Object?> get props => [ride];
}

/// Emitted by the bloc itself when the WebSocket (re)connects: the hub drops
/// per-connection subscriptions on disconnect, so the only safe recovery is
/// refetching the authoritative ride over REST and re-establishing the room.
class ReconnectedSyncRequested extends ActiveRideEvent {
  const ReconnectedSyncRequested();
}

/// Loads whatever active ride the server currently has assigned to this
/// driver (cold start / crash recovery via GET /nidus/drivers/active-ride).
class ActiveRideRefreshRequested extends ActiveRideEvent {
  const ActiveRideRefreshRequested();
}

class ClearActiveRide extends ActiveRideEvent {
  const ClearActiveRide();
}

// States
abstract class ActiveRideState extends Equatable {
  const ActiveRideState();

  @override
  List<Object?> get props => [];
}

class ActiveRideInitial extends ActiveRideState {
  const ActiveRideInitial();
}

class ActiveRideLoading extends ActiveRideState {
  const ActiveRideLoading();
}

class ActiveRideLoaded extends ActiveRideState {
  final ActiveRide ride;

  const ActiveRideLoaded({required this.ride});

  @override
  List<Object?> get props => [ride];
}

class ActiveRideCompleted extends ActiveRideState {
  final ActiveRide ride;

  const ActiveRideCompleted({required this.ride});

  @override
  List<Object?> get props => [ride];
}

class ActiveRideError extends ActiveRideState {
  final String message;

  const ActiveRideError({required this.message});

  @override
  List<Object?> get props => [message];
}

// BLoC
class ActiveRideBloc extends Bloc<ActiveRideEvent, ActiveRideState> {
  final AcceptRideUseCase _acceptRideUseCase;
  final StartRideUseCase _startRideUseCase;
  final CompleteRideUseCase _completeRideUseCase;
  final LocationService _locationService;
  // Optional for backwards-compatible construction (tests / older call
  // sites); required for the WS subscription + reconnect resync below.
  final RideRepository? _repository;

  StreamSubscription<ActiveRide>? _rideUpdateSubscription;
  StreamSubscription<bool>? _reconnectSubscription;

  /// The last authoritative snapshot — used as the merge base for partial
  /// WS ride_update payloads and re-bound whenever it changes so updates
  /// always apply onto current server truth instead of a stale capture.
  ActiveRide? _currentRide;

  ActiveRideBloc({
    required AcceptRideUseCase acceptRideUseCase,
    required StartRideUseCase startRideUseCase,
    required CompleteRideUseCase completeRideUseCase,
    required LocationService locationService,
    RideRepository? repository,
  })  : _acceptRideUseCase = acceptRideUseCase,
        _startRideUseCase = startRideUseCase,
        _completeRideUseCase = completeRideUseCase,
        _locationService = locationService,
        _repository = repository,
        super(const ActiveRideInitial()) {
    on<AcceptRideRequested>(_onAcceptRideRequested);
    on<StartRideRequested>(_onStartRideRequested);
    on<CompleteRideRequested>(_onCompleteRideRequested);
    on<RideUpdated>(_onRideUpdated);
    on<ReconnectedSyncRequested>(_onReconnectedSync);
    on<ActiveRideRefreshRequested>(_onRefreshRequested);
    on<ClearActiveRide>(_onClearActiveRide);

    _bindUpdates();
    if (_repository != null) {
      _reconnectSubscription =
          _repository!.connectionReconnects?.listen((_) {
        add(const ReconnectedSyncRequested());
      });
    }
  }

  /// (Re)subscribes getRideUpdates() against the current authoritative base.
  void _bindUpdates() {
    _rideUpdateSubscription?.cancel();
    final repo = _repository;
    if (repo == null) return;
    _rideUpdateSubscription = repo.getRideUpdates(_currentRide).listen(
      (ride) => add(RideUpdated(ride: ride)),
      // A dropped/malformed event must never wedge the bloc: the next
      // reconnect resync or manual refresh restores convergence.
      onError: (Object _) {},
      cancelOnError: false,
    );
  }

  void _remember(ActiveRide ride) {
    _currentRide = ride;
    _bindUpdates();
  }

  Future<void> _onAcceptRideRequested(
    AcceptRideRequested event,
    Emitter<ActiveRideState> emit,
  ) async {
    emit(const ActiveRideLoading());

    try {
      final ride = await _acceptRideUseCase.execute(event.rideId);
      _remember(ride);
      emit(ActiveRideLoaded(ride: ride));
    } catch (e) {
      emit(ActiveRideError(message: e.toString()));
    }
  }

  Future<void> _onStartRideRequested(
    StartRideRequested event,
    Emitter<ActiveRideState> emit,
  ) async {
    emit(const ActiveRideLoading());

    try {
      final ride = await _startRideUseCase.execute(event.rideId);
      _remember(ride);
      emit(ActiveRideLoaded(ride: ride));
    } catch (e) {
      emit(ActiveRideError(message: e.toString()));
    }
  }

  Future<void> _onCompleteRideRequested(
    CompleteRideRequested event,
    Emitter<ActiveRideState> emit,
  ) async {
    emit(const ActiveRideLoading());

    try {
      final ride = await _completeRideUseCase.execute(
        event.rideId,
        event.tipAmount,
      );
      _remember(ride);
      emit(ActiveRideCompleted(ride: ride));
    } catch (e) {
      emit(ActiveRideError(message: e.toString()));
    }
  }

  Future<void> _onRideUpdated(
    RideUpdated event,
    Emitter<ActiveRideState> emit,
  ) async {
    // Stale-event guard: ignore anything that does not belong to the ride
    // this driver currently holds (late events from a previous ride, or
    // foreign rides that slipped past transport filtering).
    final current = _currentRide;
    if (current != null && event.ride.id != current.id) {
      return;
    }
    _remember(event.ride);
    if (event.ride.isCompleted) {
      emit(ActiveRideCompleted(ride: event.ride));
    } else if (event.ride.isTerminal) {
      // Cancelled while the driver screen is open: converge to a cleared,
      // honest state rather than keeping a phantom active ride.
      _currentRide = null;
      _bindUpdates();
      emit(const ActiveRideInitial());
    } else {
      emit(ActiveRideLoaded(ride: event.ride));
    }
  }

  Future<void> _onReconnectedSync(
    ReconnectedSyncRequested event,
    Emitter<ActiveRideState> emit,
  ) async {
    await _resyncFromServer(emit);
  }

  Future<void> _onRefreshRequested(
    ActiveRideRefreshRequested event,
    Emitter<ActiveRideState> emit,
  ) async {
    await _resyncFromServer(emit);
  }

  Future<void> _resyncFromServer(Emitter<ActiveRideState> emit) async {
    final repo = _repository;
    if (repo == null) return;
    try {
      final ride = await repo.getActiveRide();
      if (ride == null) {
        // Server says no live assignment: drop any locally-held ride so the
        // UI cannot linger on a stale state indefinitely (Phase 6 gate).
        _currentRide = null;
        _bindUpdates();
        emit(const ActiveRideInitial());
        return;
      }
      _remember(ride);
      if (ride.isTerminal) {
        emit(ActiveRideCompleted(ride: ride));
      } else {
        emit(ActiveRideLoaded(ride: ride));
      }
    } catch (e) {
      emit(ActiveRideError(message: e.toString()));
    }
  }

  Future<void> _onClearActiveRide(
    ClearActiveRide event,
    Emitter<ActiveRideState> emit,
  ) async {
    _currentRide = null;
    _bindUpdates();
    emit(const ActiveRideInitial());
  }

  @override
  Future<void> close() async {
    await _rideUpdateSubscription?.cancel();
    await _reconnectSubscription?.cancel();
    return super.close();
  }
}