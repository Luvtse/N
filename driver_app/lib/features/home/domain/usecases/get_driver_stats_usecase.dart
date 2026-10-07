import '../../data/repositories/driver_home_repository.dart';
import '../../entities/driver_stats.dart';

class GetDriverStatsUseCase {
  final DriverHomeRepository _repository;

  GetDriverStatsUseCase(this._repository);

  Future<DriverStats> execute() async {
    return await _repository.getDriverStats();
  }
}