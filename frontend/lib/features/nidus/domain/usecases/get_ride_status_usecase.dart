import '../../data/repositories/ride_repository.dart';

/// Use case for getting ride status
class GetRideStatusUseCase {
  final RideRepository _rideRepository;

  const GetRideStatusUseCase(this._rideRepository);

  /// Execute the use case
  ///
  /// Parameters:
  /// - [rideId]: Ride identifier
  ///
  /// Returns:
  /// - [Ride]: Ride entity with current status
  ///
  /// Throws:
  /// - [Exception]: If ride not found or request fails
  Future<Ride> execute(String rideId) async {
    // Validate input
    if (rideId.isEmpty) {
      throw Exception('Ride ID is required');
    }

    // Get ride status from repository
    try {
      final ride = await _rideRepository.getRide(rideId);
      
      if (ride == null) {
        throw Exception('Ride not found');
      }

      return ride;
    } catch (e) {
      throw Exception('Failed to get ride status: ${e.toString()}');
    }
  }

  /// Get active ride for current driver
  ///
  /// Returns:
  /// - [Ride?]: Active ride or null if no active ride
  Future<Ride?> getActiveRide() async {
    try {
      return await _rideRepository.getActiveRide();
    } catch (e) {
      throw Exception('Failed to get active ride: ${e.toString()}');
    }
  }

  /// Check if ride is in a specific status
  ///
  /// Parameters:
  /// - [rideId]: Ride identifier
  /// - [status]: Status to check
  ///
  /// Returns:
  /// - [bool]: True if ride is in the specified status
  Future<bool> isRideInStatus(String rideId, String status) async {
    try {
      final ride = await execute(rideId);
      return ride.status == status;
    } catch (e) {
      return false;
    }
  }

  /// Check if ride is completed
  Future<bool> isRideCompleted(String rideId) async {
    return await isRideInStatus(rideId, 'completed');
  }

  /// Check if ride is cancelled
  Future<bool> isRideCancelled(String rideId) async {
    return await isRideInStatus(rideId, 'cancelled');
  }

  /// Check if ride is in progress
  Future<bool> isRideInProgress(String rideId) async {
    return await isRideInStatus(rideId, 'in_progress');
  }
}