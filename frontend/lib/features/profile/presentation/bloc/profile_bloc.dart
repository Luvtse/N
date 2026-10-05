import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:equatable/equatable.dart';

import '../../data/repositories/profile_repository.dart';
import '../../domain/entities/user_profile.dart';

// Events
abstract class ProfileEvent extends Equatable {
  const ProfileEvent();

  @override
  List<Object?> get props => [];
}

class LoadProfile extends ProfileEvent {
  const LoadProfile();
}

class UpdateProfile extends ProfileEvent {
  final Map<String, dynamic> data;

  const UpdateProfile({required this.data});

  @override
  List<Object?> get props => [data];
}

class UpdateProfilePicture extends ProfileEvent {
  final String imagePath;

  const UpdateProfilePicture({required this.imagePath});

  @override
  List<Object?> get props => [imagePath];
}

// States
abstract class ProfileState extends Equatable {
  const ProfileState();

  @override
  List<Object?> get props => [];
}

class ProfileInitial extends ProfileState {
  const ProfileInitial();
}

class ProfileLoading extends ProfileState {
  const ProfileLoading();
}

class ProfileLoaded extends ProfileState {
  final UserProfile profile;

  const ProfileLoaded({required this.profile});

  @override
  List<Object?> get props => [profile];
}

class ProfileUpdated extends ProfileState {
  final UserProfile profile;
  final String message;

  const ProfileUpdated({
    required this.profile,
    required this.message,
  });

  @override
  List<Object?> get props => [profile, message];
}

class ProfileError extends ProfileState {
  final String message;

  const ProfileError({required this.message});

  @override
  List<Object?> get props => [message];
}

// BLoC
class ProfileBloc extends Bloc<ProfileEvent, ProfileState> {
  final ProfileRepository _profileRepository;

  ProfileBloc({
    required ProfileRepository profileRepository,
  })  : _profileRepository = profileRepository,
        super(const ProfileInitial()) {
    on<LoadProfile>(_onLoadProfile);
    on<UpdateProfile>(_onUpdateProfile);
    on<UpdateProfilePicture>(_onUpdateProfilePicture);
  }

  Future<void> _onLoadProfile(
    LoadProfile event,
    Emitter<ProfileState> emit,
  ) async {
    emit(const ProfileLoading());

    try {
      final profile = await _profileRepository.getProfile();
      emit(ProfileLoaded(profile: profile));
    } catch (e) {
      emit(ProfileError(message: e.toString()));
    }
  }

  Future<void> _onUpdateProfile(
    UpdateProfile event,
    Emitter<ProfileState> emit,
  ) async {
    emit(const ProfileLoading());

    try {
      final profile = await _profileRepository.updateProfile(event.data);
      emit(ProfileUpdated(
        profile: profile,
        message: 'Profile updated successfully',
      ));
    } catch (e) {
      emit(ProfileError(message: e.toString()));
    }
  }

  Future<void> _onUpdateProfilePicture(
    UpdateProfilePicture event,
    Emitter<ProfileState> emit,
  ) async {
    emit(const ProfileLoading());

    try {
      final profile = await _profileRepository.updateProfilePicture(event.imagePath);
      emit(ProfileUpdated(
        profile: profile,
        message: 'Profile picture updated successfully',
      ));
    } catch (e) {
      emit(ProfileError(message: e.toString()));
    }
  }
}