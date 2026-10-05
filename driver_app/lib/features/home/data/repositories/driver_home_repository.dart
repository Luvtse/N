import '../../../../core/network/api_client.dart';
import '../../../../core/network/websocket_client.dart';
import '../../domain/entities/driver_stats.dart';

abstract class DriverHomeRepository {
  Future<DriverStats> getDriverStats();
  Future<void> toggleAvailability(bool isOnline);
  Future<void> updateLocation(double lat, double lng, double heading, double speed);
  Stream<Map<String, dynamic>> getRideRequests();
}

class DriverHomeRepositoryImpl implements DriverHomeRepository {
  final ApiClient _apiClient;
  final WebSocketClient _webSocketClient;

  DriverHomeRepositoryImpl({
    required ApiClient apiClient,
    required WebSocketClient webSocketClient,
  })  : _apiClient = apiClient,
        _webSocketClient = webSocketClient;

  @override
  Future<DriverStats> getDriverStats() async {
    try {
      final response = await _apiClient.get('/api/v1/drivers/stats');
      final data = response.data as Map<String, dynamic>;
      return DriverStats.fromJson(data);
    } catch (e) {
      throw Exception('Failed to get driver stats: ${e.toString()}');
    }
  }

  @override
  Future<void> toggleAvailability(bool isOnline) async {
    try {
      await _apiClient.patch(
        '/api/v1/drivers/status',
        data: {'status': isOnline ? 'available' : 'offline'},
      );
    } catch (e) {
      throw Exception('Failed to toggle availability: ${e.toString()}');
    }
  }

  @override
  Future<void> updateLocation(
    double lat,
    double lng,
    double heading,
    double speed,
  ) async {
    try {
      _webSocketClient.send({
        'type': 'location_update',
        'data': {
          'latitude': lat,
          'longitude': lng,
          'heading': heading,
          'speed': speed,
          'timestamp': DateTime.now().toIso8601String(),
        },
      });
    } catch (e) {
      throw Exception('Failed to update location: ${e.toString()}');
    }
  }

  @override
  Stream<Map<String, dynamic>> getRideRequests() {
    return _webSocketClient.messageStream.where(
      (message) => message['type'] == 'ride_request',
    );
  }
}