import '../../data/repositories/ride_repository.dart';
import '../../entities/active_ride.dart';

class CompleteRideUseCase {
  final RideRepository _repository;

  CompleteRideUseCase(this._repository);

  Future<ActiveRide> execute(String rideId, double tipAmount) async {
    if (rideId.isEmpty) {
      throw Exception('Ride ID is required');
    }

    if (tipAmount < 0) {
      throw Exception('Tip amount cannot be negative');
    }

    return await _repository.completeRide(rideId, tipAmount);
  }
}