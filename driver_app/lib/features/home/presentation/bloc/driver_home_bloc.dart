import 'dart:async';

import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:equatable/equatable.dart';
import 'package:geolocator/geolocator.dart';

import '../../../../core/network/location_service.dart';
import '../../domain/entities/driver_stats.dart';
import '../../domain/usecases/get_driver_stats_usecase.dart';
import '../../domain/usecases/toggle_availability_usecase.dart';

// Events
abstract class DriverHomeEvent extends Equatable {
  const DriverHomeEvent();

  @override
  List<Object?> get props => [];
}

class LoadDriverStats extends DriverHomeEvent {
  const LoadDriverStats();
}

class ToggleAvailability extends DriverHomeEvent {
  final bool isOnline;

  const ToggleAvailability({required this.isOnline});

  @override
  List<Object?> get props => [isOnline];
}

class StartLocationTracking extends DriverHomeEvent {
  const StartLocationTracking();
}

class StopLocationTracking extends DriverHomeEvent {
  const StopLocationTracking();
}

class LocationUpdated extends DriverHomeEvent {
  final Position position;

  const LocationUpdated({required this.position});

  @override
  List<Object?> get props => [position];
}

// States
abstract class DriverHomeState extends Equatable {
  const DriverHomeState();

  @override
  List<Object?> get props => [];
}

class DriverHomeInitial extends DriverHomeState {
  const DriverHomeInitial();
}

class DriverHomeLoading extends DriverHomeState {
  const DriverHomeLoading();
}

class DriverHomeLoaded extends DriverHomeState {
  final DriverStats stats;
  final bool isOnline;
  final bool isTracking;
  final Position? currentPosition;

  const DriverHomeLoaded({
    required this.stats,
    required this.isOnline,
    required this.isTracking,
    this.currentPosition,
  });

  DriverHomeLoaded copyWith({
    DriverStats? stats,
    bool? isOnline,
    bool? isTracking,
    Position? currentPosition,
  }) {
    return DriverHomeLoaded(
      stats: stats ?? this.stats,
      isOnline: isOnline ?? this.isOnline,
      isTracking: isTracking ?? this.isTracking,
      currentPosition: currentPosition ?? this.currentPosition,
    );
  }

  @override
  List<Object?> get props => [stats, isOnline, isTracking, currentPosition];
}

class DriverHomeError extends DriverHomeState {
  final String message;

  const DriverHomeError({required this.message});

  @override
  List<Object?> get props => [message];
}

// BLoC
class DriverHomeBloc extends Bloc<DriverHomeEvent, DriverHomeState> {
  final GetDriverStatsUseCase _getDriverStatsUseCase;
  final ToggleAvailabilityUseCase _toggleAvailabilityUseCase;
  final LocationService _locationService;

  StreamSubscription<Position>? _locationSubscription;

  DriverHomeBloc({
    required GetDriverStatsUseCase getDriverStatsUseCase,
    required ToggleAvailabilityUseCase toggleAvailabilityUseCase,
    required LocationService locationService,
  })  : _getDriverStatsUseCase = getDriverStatsUseCase,
        _toggleAvailabilityUseCase = toggleAvailabilityUseCase,
        _locationService = locationService,
        super(const DriverHomeInitial()) {
    on<LoadDriverStats>(_onLoadDriverStats);
    on<ToggleAvailability>(_onToggleAvailability);
    on<StartLocationTracking>(_onStartLocationTracking);
    on<StopLocationTracking>(_onStopLocationTracking);
    on<LocationUpdated>(_onLocationUpdated);
  }

  Future<void> _onLoadDriverStats(
    LoadDriverStats event,
    Emitter<DriverHomeState> emit,
  ) async {
    emit(const DriverHomeLoading());

    try {
      final stats = await _getDriverStatsUseCase.execute();

      if (state is DriverHomeLoaded) {
        final currentState = state as DriverHomeLoaded;
        emit(currentState.copyWith(stats: stats));
      } else {
        emit(DriverHomeLoaded(
          stats: stats,
          isOnline: false,
          isTracking: false,
        ));
      }
    } catch (e) {
      emit(DriverHomeError(message: e.toString()));
    }
  }

  Future<void> _onToggleAvailability(
    ToggleAvailability event,
    Emitter<DriverHomeState> emit,
  ) async {
    try {
      await _toggleAvailabilityUseCase.execute(event.isOnline);

      if (state is DriverHomeLoaded) {
        final currentState = state as DriverHomeLoaded;
        emit(currentState.copyWith(isOnline: event.isOnline));

        // Start/stop location tracking based on availability
        if (event.isOnline) {
          add(const StartLocationTracking());
        } else {
          add(const StopLocationTracking());
        }
      }
    } catch (e) {
      emit(DriverHomeError(message: e.toString()));
    }
  }

  Future<void> _onStartLocationTracking(
    StartLocationTracking event,
    Emitter<DriverHomeState> emit,
  ) async {
    try {
      await _locationService.startTracking();

      _locationSubscription = _locationService.locationStream.listen(
        (position) {
          add(LocationUpdated(position: position));
        },
      );

      if (state is DriverHomeLoaded) {
        final currentState = state as DriverHomeLoaded;
        emit(currentState.copyWith(isTracking: true));
      }
    } catch (e) {
      emit(DriverHomeError(message: e.toString()));
    }
  }

  Future<void> _onStopLocationTracking(
    StopLocationTracking event,
    Emitter<DriverHomeState> emit,
  ) async {
    await _locationSubscription?.cancel();
    _locationSubscription = null;
    await _locationService.stopTracking();

    if (state is DriverHomeLoaded) {
      final currentState = state as DriverHomeLoaded;
      emit(currentState.copyWith(isTracking: false));
    }
  }

  Future<void> _onLocationUpdated(
    LocationUpdated event,
    Emitter<DriverHomeState> emit,
  ) async {
    if (state is DriverHomeLoaded) {
      final currentState = state as DriverHomeLoaded;
      
      // Send location to backend
      await _locationService.updateLocation(
        event.position.latitude,
        event.position.longitude,
        event.position.heading,
        event.position.speed,
      );

      emit(currentState.copyWith(currentPosition: event.position));
    }
  }

  @override
  Future<void> close() async {
    await _locationSubscription?.cancel();
    await _locationService.dispose();
    return super.close();
  }
}