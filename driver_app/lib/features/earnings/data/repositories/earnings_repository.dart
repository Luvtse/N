import '../../../../core/network/api_client.dart';
import '../../domain/entities/earnings_report.dart';

abstract class EarningsRepository {
  Future<EarningsReport> getDailyEarnings(DateTime date);
  Future<EarningsReport> getWeeklyEarnings(DateTime startDate);
  Future<EarningsReport> getMonthlyEarnings(int year, int month);
}

class EarningsRepositoryImpl implements EarningsRepository {
  final ApiClient _apiClient;

  EarningsRepositoryImpl({required ApiClient apiClient}) : _apiClient = apiClient;

  @override
  Future<EarningsReport> getDailyEarnings(DateTime date) async {
    try {
      final response = await _apiClient.get(
        '/api/v1/drivers/earnings/daily',
        queryParameters: {'date': date.toIso8601String().split('T')[0]},
      );
      final data = response.data as Map<String, dynamic>;
      return EarningsReport.fromJson(data);
    } catch (e) {
      throw Exception('Failed to get daily earnings: ${e.toString()}');
    }
  }

  @override
  Future<EarningsReport> getWeeklyEarnings(DateTime startDate) async {
    try {
      final response = await _apiClient.get(
        '/api/v1/drivers/earnings/weekly',
        queryParameters: {'start_date': startDate.toIso8601String().split('T')[0]},
      );
      final data = response.data as Map<String, dynamic>;
      return EarningsReport.fromJson(data);
    } catch (e) {
      throw Exception('Failed to get weekly earnings: ${e.toString()}');
    }
  }

  @override
  Future<EarningsReport> getMonthlyEarnings(int year, int month) async {
    try {
      final response = await _apiClient.get(
        '/api/v1/drivers/earnings/monthly',
        queryParameters: {'year': year, 'month': month},
      );
      final data = response.data as Map<String, dynamic>;
      return EarningsReport.fromJson(data);
    } catch (e) {
      throw Exception('Failed to get monthly earnings: ${e.toString()}');
    }
  }
}