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

  // Fires `true` after EVERY successful (re)connect so feature layers can
  // trigger a REST resync — the backend hub drops in-flight topics on
  // disconnect, so clients must re-subscribe and refetch authoritative state.
  final StreamController<bool> _connectionStateController =
      StreamController<bool>.broadcast();
  Stream<bool> get connectionStateStream => _connectionStateController.stream;
  bool get isConnected => _isConnected;

  // Topics the app wants joined server-side. The hub authorizes every
  // subscription (Phase 4); we replay this set after each (re)connect because
  // client topics are per-connection on the server.
  final Set<String> _topics = <String>{};

  bool _isConnected = false;
  Timer? _heartbeatTimer;

  WebSocketClient({required this.baseUrl});

  /// Sends {"type":"subscribe","payload":{"topic":...}} exactly as the
  /// backend Client.handleMessage contract expects
  /// (websocket_handler.go case "subscribe"). Remembered even while offline:
  /// flushed automatically on the next successful connect.
  void subscribe(String topic) {
    if (topic.isEmpty) return;
    _topics.add(topic);
    send({
      'type': 'subscribe',
      'payload': {'topic': topic},
      'timestamp': DateTime.now().millisecondsSinceEpoch ~/ 1000,
    });
  }

  void unsubscribe(String topic) {
    _topics.remove(topic);
    send({
      'type': 'unsubscribe',
      'payload': {'topic': topic},
      'timestamp': DateTime.now().millisecondsSinceEpoch ~/ 1000,
    });
  }

  void _resubscribeAll() {
    for (final topic in _topics.toList()) {
      send({
        'type': 'subscribe',
        'payload': {'topic': topic},
        'timestamp': DateTime.now().millisecondsSinceEpoch ~/ 1000,
      });
    }
  }

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
      // Rejoin every remembered topic: server-side subscriptions die with
      // the connection, so a reconnect without replay silently loses all
      // ride updates (Phase 6 convergence fix).
      _resubscribeAll();
      _connectionStateController.add(true);

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
    _connectionStateController.close();
  }
}