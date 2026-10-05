import '../data/repositories/driver_home_repository.dart';

class ToggleAvailabilityUseCase {
  final DriverHomeRepository _repository;

  ToggleAvailabilityUseCase(this._repository);

  Future<void> execute(bool isOnline) async {
    await _repository.toggleAvailability(isOnline);
  }
}