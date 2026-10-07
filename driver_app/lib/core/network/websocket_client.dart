import 'dart:async';
import 'dart:convert';
import 'package:web_socket_channel/web_socket_channel.dart';
import 'package:web_socket_channel/io.dart';
import 'package:hive/hive.dart';

class WebSocketClient {
  WebSocketChannel? _channel;
  final String baseUrl;
  final StreamController<Map<String, dynamic>> _messageController = 
      StreamController.broadcast();
  
  Stream<Map<String, dynamic>> get messageStream => _messageController.stream;
  
  bool _isConnected = false;
  Timer? _heartbeatTimer;

  WebSocketClient({required this.baseUrl});

  Future<void> connect() async {
    try {
      final authBox = await Hive.openBox('driver_auth');
      final token = authBox.get('access_token');
      
      if (token == null) {
        throw Exception('No authentication token found');
      }

      final wsUrl = baseUrl.replaceAll('http', 'ws');
      // Phase B/B6: pass the JWT via the Sec-WebSocket-Protocol subprotocol
      // ("nidaw-auth.<jwt>") instead of a ?token= query param, so access
      // logs / proxies never see the credential. IOWebSocketChannel lets us
      // set the handshake header directly.
      _channel = IOWebSocketChannel.connect(
        Uri.parse('$wsUrl/ws?type=driver'),
        protocols: ['nidaw-auth.$token'],
      );

      await _channel!.ready;
      _isConnected = true;

      _channel!.stream.listen(
        (message) {
          try {
            final data = jsonDecode(message) as Map<String, dynamic>;
            _messageController.add(data);
          } catch (e) {
            print('Failed to parse WebSocket message: $e');
          }
        },
        onError: (error) {
          print('WebSocket error: $error');
          _isConnected = false;
          _scheduleReconnect();
        },
        onDone: () {
          print('WebSocket connection closed');
          _isConnected = false;
          _scheduleReconnect();
        },
      );

      _startHeartbeat();
      print('✅ WebSocket connected');
    } catch (e) {
      print('❌ WebSocket connection failed: $e');
      _scheduleReconnect();
    }
  }

  void _scheduleReconnect() {
    Future.delayed(const Duration(seconds: 5), () {
      if (!_isConnected) {
        connect();
      }
    });
  }

  void _startHeartbeat() {
    _heartbeatTimer?.cancel();
    _heartbeatTimer = Timer.periodic(const Duration(seconds: 30), (_) {
      if (_isConnected) {
        send({'type': 'heartbeat'});
      }
    });
  }

  void send(Map<String, dynamic> message) {
    if (_isConnected && _channel != null) {
      _channel!.sink.add(jsonEncode(message));
    }
  }

  void disconnect() {
    _heartbeatTimer?.cancel();
    _channel?.sink.close();
    _isConnected = false;
  }

  void dispose() {
    disconnect();
    _messageController.close();
  }
}