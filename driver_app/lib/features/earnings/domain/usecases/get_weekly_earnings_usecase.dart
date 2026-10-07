import '../../data/repositories/earnings_repository.dart';
import '../../entities/earnings_report.dart';

class GetWeeklyEarningsUseCase {
  final EarningsRepository _repository;

  GetWeeklyEarningsUseCase(this._repository);

  Future<EarningsReport> execute(DateTime startDate) async {
    return await _repository.getWeeklyEarnings(startDate);
  }
}