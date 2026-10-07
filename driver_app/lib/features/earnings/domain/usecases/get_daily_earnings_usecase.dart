import '../../data/repositories/earnings_repository.dart';
import '../../entities/earnings_report.dart';

class GetDailyEarningsUseCase {
  final EarningsRepository _repository;

  GetDailyEarningsUseCase(this._repository);

  Future<EarningsReport> execute(DateTime date) async {
    return await _repository.getDailyEarnings(date);
  }
}