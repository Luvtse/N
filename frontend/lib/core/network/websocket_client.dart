import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:web_socket_channel/web_socket_channel.dart';
import 'package:web_socket_channel/io.dart' if (dart.library.html) 'package:web_socket_channel/html.dart';

/// WebSocket client for real-time communication
class WebSocketClient {
  WebSocketChannel? _channel;
  final String _baseUrl;
  final String Function()? _tokenProvider;
  
  final StreamController<WebSocketMessage> _messageController = 
      StreamController.broadcast();
  final StreamController<WebSocketConnectionState> _stateController = 
      StreamController.broadcast();
  
  final Map<String, List<Function(dynamic)>> _subscriptions = {};
  
  bool _isConnected = false;
  int _reconnectAttempts = 0;
  Timer? _heartbeatTimer;
  Timer? _reconnectTimer;

  WebSocketClient({
    required String baseUrl,
    String Function()? tokenProvider,
  })  : _baseUrl = baseUrl,
        _tokenProvider = tokenProvider;

  // ==========================================================================
  // STREAMS
  // ==========================================================================

  Stream<WebSocketMessage> get messageStream => _messageController.stream;
  Stream<WebSocketConnectionState> get stateStream => _stateController.stream;
  bool get isConnected => _isConnected;

  // ==========================================================================
  // CONNECTION MANAGEMENT
  // ==========================================================================

  Future<void> connect() async {
    if (_isConnected) {
      return;
    }

    try {
      _emitState(WebSocketConnectionState.connecting);

      final token = _tokenProvider?.call();
      // Phase B/B6: never put the JWT in the URL (?token= leaks into access
      // logs/proxies). Pass it via the Sec-WebSocket-Protocol subprotocol
      // "nidaw-auth.<jwt>", which the backend hub extracts first.
      if (kIsWeb) {
        // Browser WebSocket API cannot set handshake headers/subprotocols for
        // custom auth; fall back to the deprecated query param only on web,
        // where the server logs a migration warning.
        final url = token != null ? '$_baseUrl?token=$token' : _baseUrl;
        _channel = WebSocketChannel.connect(Uri.parse(url));
      } else {
        _channel = IOWebSocketChannel.connect(
          Uri.parse(_baseUrl),
          protocols: token != null ? ['nidaw-auth.$token'] : null,
        );
      }

      await _channel!.ready;

      _isConnected = true;
      _reconnectAttempts = 0;
      _emitState(WebSocketConnectionState.connected);

      _startHeartbeat();
      _listenToMessages();

    } catch (e) {
      debugPrint('WebSocket connection failed: $e');
      _emitState(WebSocketConnectionState.error);
      _scheduleReconnect();
    }
  }

  Future<void> disconnect() async {
    _isConnected = false;
    _heartbeatTimer?.cancel();
    _reconnectTimer?.cancel();
    
    await _channel?.sink.close();
    _channel = null;
    
    _emitState(WebSocketConnectionState.disconnected);
  }

  void _listenToMessages() {
    _channel?.stream.listen(
      (data) {
        try {
          final message = WebSocketMessage.fromJson(jsonDecode(data));
          _messageController.add(message);
          _notifySubscribers(message);
        } catch (e) {
          debugPrint('Failed to parse WebSocket message: $e');
        }
      },
      onError: (error) {
        debugPrint('WebSocket error: $error');
        _emitState(WebSocketConnectionState.error);
        _scheduleReconnect();
      },
      onDone: () {
        debugPrint('WebSocket connection closed');
        _isConnected = false;
        _emitState(WebSocketConnectionState.disconnected);
        _scheduleReconnect();
      },
    );
  }

  void _scheduleReconnect() {
    if (_reconnectAttempts >= 5) {
      debugPrint('Max reconnection attempts reached');
      return;
    }

    _reconnectAttempts++;
    final delay = Duration(seconds: _reconnectAttempts * 2);
    
    _reconnectTimer?.cancel();
    _reconnectTimer = Timer(delay, () {
      debugPrint('Attempting to reconnect...');
      connect();
    });
  }

  void _startHeartbeat() {
    _heartbeatTimer?.cancel();
    _heartbeatTimer = Timer.periodic(const Duration(seconds: 30), (_) {
      if (_isConnected) {
        send(WebSocketMessage(
          type: 'heartbeat',
          data: {'timestamp': DateTime.now().toIso8601String()},
        ));
      }
    });
  }

  // ==========================================================================
  // SUBSCRIPTIONS
  // ==========================================================================

  void subscribe(String topic, Function(dynamic) callback) {
    _subscriptions.putIfAbsent(topic, () => []);
    _subscriptions[topic]!.add(callback);

    if (_isConnected) {
      send(WebSocketMessage(
        type: 'subscribe',
        data: {'topic': topic},
      ));
    }
  }

  void unsubscribe(String topic) {
    _subscriptions.remove(topic);

    if (_isConnected) {
      send(WebSocketMessage(
        type: 'unsubscribe',
        data: {'topic': topic},
      ));
    }
  }

  void _notifySubscribers(WebSocketMessage message) {
    final topic = message.data?['topic'] as String?;
    if (topic != null && _subscriptions.containsKey(topic)) {
      for (final callback in _subscriptions[topic]!) {
        try {
          callback(message.data);
        } catch (e) {
          debugPrint('Error in subscription callback: $e');
        }
      }
    }
  }

  // ==========================================================================
  // SEND MESSAGES
  // ==========================================================================

  void send(WebSocketMessage message) {
    if (!_isConnected || _channel == null) {
      debugPrint('Cannot send message: WebSocket not connected');
      return;
    }

    try {
      _channel!.sink.add(jsonEncode(message.toJson()));
    } catch (e) {
      debugPrint('Failed to send WebSocket message: $e');
    }
  }

  // ==========================================================================
  // HELPERS
  // ==========================================================================

  void _emitState(WebSocketConnectionState state) {
    _stateController.add(state);
  }

  void dispose() {
    disconnect();
    _messageController.close();
    _stateController.close();
  }
}

// ============================================================================
// MODELS
// ============================================================================

class WebSocketMessage {
  final String type;
  final Map<String, dynamic>? data;
  final DateTime timestamp;

  WebSocketMessage({
    required this.type,
    this.data,
    DateTime? timestamp,
  }) : timestamp = timestamp ?? DateTime.now();

  factory WebSocketMessage.fromJson(Map<String, dynamic> json) {
    return WebSocketMessage(
      type: json['type'] as String,
      data: json['data'] as Map<String, dynamic>?,
      timestamp: json['timestamp'] != null
          ? DateTime.parse(json['timestamp'] as String)
          : null,
    );
  }

  Map<String, dynamic> toJson() {
    return {
      'type': type,
      if (data != null) 'data': data,
      'timestamp': timestamp.toIso8601String(),
    };
  }
}

enum WebSocketConnectionState {
  disconnected,
  connecting,
  connected,
  error,
}