import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:equatable/equatable.dart';

import '../../data/repositories/driver_profile_repository.dart';
import '../../domain/entities/driver_profile.dart';

// Events
abstract class DriverProfileEvent extends Equatable {
  const DriverProfileEvent();

  @override
  List<Object?> get props => [];
}

class LoadProfile extends DriverProfileEvent {
  const LoadProfile();
}

class UpdateProfile extends DriverProfileEvent {
  final Map<String, dynamic> data;

  const UpdateProfile({required this.data});

  @override
  List<Object?> get props => [data];
}

class UpdateVehicleInfo extends DriverProfileEvent {
  final Map<String, dynamic> data;

  const UpdateVehicleInfo({required this.data});

  @override
  List<Object?> get props => [data];
}

// States
abstract class DriverProfileState extends Equatable {
  const DriverProfileState();

  @override
  List<Object?> get props => [];
}

class DriverProfileInitial extends DriverProfileState {
  const DriverProfileInitial();
}

class DriverProfileLoading extends DriverProfileState {
  const DriverProfileLoading();
}

class DriverProfileLoaded extends DriverProfileState {
  final DriverProfile profile;

  const DriverProfileLoaded({required this.profile});

  @override
  List<Object?> get props => [profile];
}

class DriverProfileUpdated extends DriverProfileState {
  final DriverProfile profile;
  final String message;

  const DriverProfileUpdated({
    required this.profile,
    required this.message,
  });

  @override
  List<Object?> get props => [profile, message];
}

class DriverProfileError extends DriverProfileState {
  final String message;

  const DriverProfileError({required this.message});

  @override
  List<Object?> get props => [message];
}

// BLoC
class DriverProfileBloc extends Bloc<DriverProfileEvent, DriverProfileState> {
  final DriverProfileRepository _profileRepository;

  DriverProfileBloc({
    required DriverProfileRepository profileRepository,
  })  : _profileRepository = profileRepository,
        super(const DriverProfileInitial()) {
    on<LoadProfile>(_onLoadProfile);
    on<UpdateProfile>(_onUpdateProfile);
    on<UpdateVehicleInfo>(_onUpdateVehicleInfo);
  }

  Future<void> _onLoadProfile(
    LoadProfile event,
    Emitter<DriverProfileState> emit,
  ) async {
    emit(const DriverProfileLoading());

    try {
      final profile = await _profileRepository.getProfile();
      emit(DriverProfileLoaded(profile: profile));
    } catch (e) {
      emit(DriverProfileError(message: e.toString()));
    }
  }

  Future<void> _onUpdateProfile(
    UpdateProfile event,
    Emitter<DriverProfileState> emit,
  ) async {
    emit(const DriverProfileLoading());

    try {
      final profile = await _profileRepository.updateProfile(event.data);
      emit(DriverProfileUpdated(
        profile: profile,
        message: 'Profile updated successfully',
      ));
    } catch (e) {
      emit(DriverProfileError(message: e.toString()));
    }
  }

  Future<void> _onUpdateVehicleInfo(
    UpdateVehicleInfo event,
    Emitter<DriverProfileState> emit,
  ) async {
    emit(const DriverProfileLoading());

    try {
      await _profileRepository.updateVehicleInfo(event.data);
      
      // Reload profile
      final profile = await _profileRepository.getProfile();
      emit(DriverProfileUpdated(
        profile: profile,
        message: 'Vehicle info updated successfully',
      ));
    } catch (e) {
      emit(DriverProfileError(message: e.toString()));
    }
  }
}