import '../data/repositories/ride_repository.dart';
import '../entities/active_ride.dart';

class StartRideUseCase {
  final RideRepository _repository;

  StartRideUseCase(this._repository);

  Future<ActiveRide> execute(String rideId) async {
    if (rideId.isEmpty) {
      throw Exception('Ride ID is required');
    }

    return await _repository.startRide(rideId);
  }
}