import 'dart:async';

import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:equatable/equatable.dart';

import '../../../../core/network/location_service.dart';
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

  StreamSubscription<ActiveRide>? _rideUpdateSubscription;

  ActiveRideBloc({
    required AcceptRideUseCase acceptRideUseCase,
    required StartRideUseCase startRideUseCase,
    required CompleteRideUseCase completeRideUseCase,
    required LocationService locationService,
  })  : _acceptRideUseCase = acceptRideUseCase,
        _startRideUseCase = startRideUseCase,
        _completeRideUseCase = completeRideUseCase,
        _locationService = locationService,
        super(const ActiveRideInitial());

  Future<void> _onAcceptRideRequested(
    AcceptRideRequested event,
    Emitter<ActiveRideState> emit,
  ) async {
    emit(const ActiveRideLoading());

    try {
      final ride = await _acceptRideUseCase.execute(event.rideId);
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
      emit(ActiveRideCompleted(ride: ride));
    } catch (e) {
      emit(ActiveRideError(message: e.toString()));
    }
  }

  Future<void> _onRideUpdated(
    RideUpdated event,
    Emitter<ActiveRideState> emit,
  ) async {
    if (event.ride.isCompleted) {
      emit(ActiveRideCompleted(ride: event.ride));
    } else {
      emit(ActiveRideLoaded(ride: event.ride));
    }
  }

  Future<void> _onClearActiveRide(
    ClearActiveRide event,
    Emitter<ActiveRideState> emit,
  ) async {
    emit(const ActiveRideInitial());
  }

  @override
  Future<void> close() async {
    await _rideUpdateSubscription?.cancel();
    return super.close();
  }
}