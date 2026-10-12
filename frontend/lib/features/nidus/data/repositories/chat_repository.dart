// §2.1 In-app chat — rider↔driver conversation repository.
//
// Talks to the backend chat module (kill-switch: FEATURE_CHAT). Endpoints,
// all under an authenticated ApiClient:
//   POST /api/v1/chat/rides/{rideId}/messages   -> 201 Message
//   GET  /api/v1/chat/rides/{rideId}/messages   -> { messages: [Message], next_cursor }
//   GET  /api/v1/chat/rides/{rideId}/messages/unread -> { unread: int }
//   POST /api/v1/chat/rides/{rideId}/messages/read   -> { marked: int }
//   GET  /api/v1/chat/canned?locale=            -> CannedCatalog
//
// Real-time delivery uses the shared WebSocketClient topic `chat.ride.{rideId}`;
// the server stamps `topic` into every fan-out payload, which is how
// WebSocketClient._notifySubscribers routes it back to our callback.
//
// Honesty rules (CONTRIBUTING-RIDE-PARITY Global Rule 2): when the backend
// reports chat is disabled/unreachable we surface a ChatUnavailableException —
// the UI shows a real "unavailable" state with retry. We never fabricate
// messages, cursors, or canned text client-side.
import 'dart:async';

import 'package:dio/dio.dart';

import '../../../../core/network/api_client.dart';
import '../../../../core/network/websocket_client.dart';

// ============================================================================
// MODELS
// ============================================================================

enum ChatSenderRole { rider, driver, unknown }

ChatSenderRole _roleFrom(String? raw) {
  switch (raw) {
    case 'rider':
      return ChatSenderRole.rider;
    case 'driver':
      return ChatSenderRole.driver;
    default:
      return ChatSenderRole.unknown;
  }
}

/// One persisted (or optimistically pending) chat message. Field names mirror
/// the Go domain.Message JSON tags exactly.
class ChatMessage {
  final String id;
  final String rideId;
  final String senderId;
  final ChatSenderRole senderRole;
  final String body;
  final String? cannedId;

  /// Populated only when the translation flag + provider are active server
  /// side. Null means "not translated" — the UI hides translation affordances.
  final String? translatedFrom;
  final DateTime createdAt;
  final DateTime? deliveredAt;
  final DateTime? readAt;

  /// Server status: sent | delivered | read. Locally-created optimistic
  /// messages use the synthetic value 'pending' until the POST confirms.
  final String status;

  const ChatMessage({
    required this.id,
    required this.rideId,
    required this.senderId,
    required this.senderRole,
    required this.body,
    required this.createdAt,
    required this.status,
    this.cannedId,
    this.translatedFrom,
    this.deliveredAt,
    this.readAt,
  });

  bool get isPending => status == 'pending';
  bool get isFailed => status == 'failed';

  factory ChatMessage.fromJson(Map<String, dynamic> json) {
    return ChatMessage(
      id: json['id'] as String? ?? '',
      rideId: json['ride_id'] as String? ?? '',
      senderId: json['sender_id'] as String? ?? '',
      senderRole: _roleFrom(json['sender_role'] as String?),
      body: json['body'] as String? ?? '',
      cannedId: json['canned_id'] as String?,
      translatedFrom: json['translated_from'] as String?,
      createdAt:
          DateTime.tryParse(json['created_at'] as String? ?? '') ??
          DateTime.now(),
      deliveredAt: DateTime.tryParse(json['delivered_at'] as String? ?? ''),
      readAt: DateTime.tryParse(json['read_at'] as String? ?? ''),
      status: json['status'] as String? ?? 'sent',
    );
  }

  /// Optimistic local copy awaiting server confirmation.
  ChatMessage.pending({
    required String tempId,
    required String rideId,
    required ChatSenderRole senderRole,
    required String body,
    String? cannedId,
  }) : this(
          id: tempId,
          rideId: rideId,
          senderId: '',
          senderRole: senderRole,
          body: body,
          cannedId: cannedId,
          translatedFrom: null,
          createdAt: DateTime.now(),
          status: 'pending',
        );

  ChatMessage copyWithStatus(String newStatus) => ChatMessage(
        id: id,
        rideId: rideId,
        senderId: senderId,
        senderRole: senderRole,
        body: body,
        cannedId: cannedId,
        translatedFrom: translatedFrom,
        createdAt: createdAt,
        deliveredAt: deliveredAt,
        readAt: readAt,
        status: newStatus,
      );
}

/// One page of history, oldest-first, with the opaque keyset cursor for the
/// next page (empty string = end of conversation).
class ChatHistoryPage {
  final List<ChatMessage> messages;
  final String nextCursor;

  const ChatHistoryPage({required this.messages, required this.nextCursor});

  bool get hasMore => nextCursor.isNotEmpty;
}

class CannedMessage {
  final String id;
  final String label;
  final String body;

  const CannedMessage({
    required this.id,
    required this.label,
    required this.body,
  });

  factory CannedMessage.fromJson(Map<String, dynamic> json) => CannedMessage(
        id: json['id'] as String? ?? '',
        label: json['label'] as String? ?? '',
        body: json['body'] as String? ?? '',
      );
}

/// Per-role quick-reply catalog from GET /chat/canned. The rider list and the
/// driver list differ by design; callers pass their own role's list.
class CannedCatalog {
  final String locale;
  final List<CannedMessage> rider;
  final List<CannedMessage> driver;

  const CannedCatalog({
    required this.locale,
    required this.rider,
    required this.driver,
  });

  factory CannedCatalog.fromJson(Map<String, dynamic> json) {
    List<CannedMessage> parseList(dynamic raw) {
      if (raw is! List) return const [];
      return raw
          .whereType<Map<String, dynamic>>()
          .map(CannedMessage.fromJson)
          .toList(growable: false);
    }

    return CannedCatalog(
      locale: json['locale'] as String? ?? 'en',
      rider: parseList(json['rider']),
      driver: parseList(json['driver']),
    );
  }

  List<CannedMessage> forRole(ChatSenderRole role) =>
      role == ChatSenderRole.driver ? driver : rider;
}

// ============================================================================
// ERRORS
// ============================================================================

/// Chat is not available right now (feature off, 503 fail-closed rides store,
/// network failure, or unexpected status). The UI must show a real
/// "unavailable — retry" state; never fake data.
class ChatUnavailableException implements Exception {
  final String message;
  final int? statusCode;

  const ChatUnavailableException(this.message, {this.statusCode});

  @override
  String toString() => 'ChatUnavailableException($statusCode): $message';
}

/// The caller is not a participant of the ride (403). Terminal — no retry.
class ChatForbiddenException implements Exception {
  final String message;
  const ChatForbiddenException([this.message = 'not a participant of this ride']);

  @override
  String toString() => 'ChatForbiddenException: $message';
}

// ============================================================================
// REPOSITORY
// ============================================================================

typedef ChatIncomingHandler = void Function(ChatMessage message);

class ChatRepository {
  final ApiClient _api;
  final WebSocketClient _ws;

  static const String _base = '/api/v1/chat';

  ChatRepository({required ApiClient api, required WebSocketClient ws});

  // --------------------------------------------------------------------------
  // Real-time subscription
  // --------------------------------------------------------------------------

  StreamSubscription<WebSocketMessage>? _sub;
  final Map<String, ChatIncomingHandler> _handlers = {};

  /// Subscribe to live messages for one ride. Callbacks receive server-sent
  /// messages only; echo suppression of our own optimistic sends is the
  /// caller's job (match on body+cannedId or drop pending ids on confirm).
  void listen(String rideId, ChatIncomingHandler onMessage) {
    final topic = 'chat.ride.$rideId';
    _handlers[topic] = onMessage;
    _sub?.cancel();
    _sub = _ws.messageStream.listen((msg) {
      final data = msg.data;
      if (data == null) return;
      if (data['topic'] != topic) return;
      try {
        final parsed = ChatMessage.fromJson(data);
        if (parsed.id.isEmpty) return;
        onMessage(parsed);
      } catch (_) {
        // Malformed frame: ignore rather than crash the chat surface.
      }
    });
    _ws.connect();
  }

  void stopListening() {
    _sub?.cancel();
    _sub = null;
    _handlers.clear();
  }

  // --------------------------------------------------------------------------
  // REST calls
  // --------------------------------------------------------------------------

  /// Send one message. Exactly one of [body] or [cannedId] should be provided;
  /// canned-only sends are expanded server-side from the catalog.
  Future<ChatMessage> send(
    String rideId, {
    String? body,
    String? cannedId,
    String? recipientLocale,
  }) async {
    try {
      final res = await _api.post<Map<String, dynamic>>(
        '$_base/rides/$rideId/messages',
        data: {
          if (body != null) 'body': body,
          if (cannedId != null) 'canned_id': cannedId,
          if (recipientLocale != null) 'recipient_locale': recipientLocale,
        },
      );
      final json = res.data;
      if (res.statusCode != 201 || json == null) {
        throw ChatUnavailableException(
          'unexpected send response',
          statusCode: res.statusCode,
        );
      }
      return ChatMessage.fromJson(json);
    } on ChatUnavailableException {
      rethrow;
    } on DioException catch (e) {
      final code = e.response?.statusCode;
      if (code == 403) throw const ChatForbiddenException();
      throw ChatUnavailableException('send failed', statusCode: code);
    }
  }

  /// Oldest-first page of history. First call passes an empty cursor; keep
  /// calling with page.nextCursor while page.hasMore.
  Future<ChatHistoryPage> history(
    String rideId, {
    String cursor = '',
    int limit = 50,
  }) async {
    try {
      final res = await _api.get<Map<String, dynamic>>(
        '$_base/rides/$rideId/messages',
        queryParameters: {
          if (cursor.isNotEmpty) 'cursor': cursor,
          'limit': '$limit',
        },
      );
      final json = res.data;
      if (res.statusCode != 200 || json == null) {
        throw ChatUnavailableException(
          'unexpected history response',
          statusCode: res.statusCode,
        );
      }
      final items = (json['messages'] as List?) ?? const [];
      return ChatHistoryPage(
        messages: items
            .whereType<Map<String, dynamic>>()
            .map(ChatMessage.fromJson)
            .toList(),
        nextCursor: json['next_cursor'] as String? ?? '',
      );
    } on ChatUnavailableException {
      rethrow;
    } on DioException catch (e) {
      final code = e.response?.statusCode;
      if (code == 403) throw const ChatForbiddenException();
      throw ChatUnavailableException('history fetch failed', statusCode: code);
    }
  }

  /// Unread count for the badge on the Tracking-page chat entry point.
  Future<int> unreadCount(String rideId) async {
    try {
      final res = await _api.get<Map<String, dynamic>>(
        '$_base/rides/$rideId/messages/unread',
      );
      final json = res.data;
      if (res.statusCode != 200 || json == null) {
        throw ChatUnavailableException(
          'unexpected unread response',
          statusCode: res.statusCode,
        );
      }
      return (json['unread'] as num?)?.toInt() ?? 0;
    } on DioException catch (e) {
      final code = e.response?.statusCode;
      if (code == 403) throw const ChatForbiddenException();
      throw ChatUnavailableException('unread fetch failed', statusCode: code);
    }
  }

  /// Stamp read_at on the other participant's messages. Returns how many were
  /// marked; failures are non-fatal for the UI (badge just lags).
  Future<int> markRead(String rideId) async {
    try {
      final res = await _api.post<Map<String, dynamic>>(
        '$_base/rides/$rideId/messages/read',
      );
      final json = res.data;
      if (json == null) return 0;
      return (json['marked'] as num?)?.toInt() ?? 0;
    } on DioException {
      return 0;
    }
  }

  /// Static per-locale canned catalog. Falls back to en server-side for
  /// unknown locales, so a non-empty catalog is expected whenever chat is on.
  Future<CannedCatalog> canned({String locale = 'en'}) async {
    try {
      final res = await _api.get<Map<String, dynamic>>(
        '$_base/canned',
        queryParameters: {'locale': locale},
      );
      final json = res.data;
      if (res.statusCode != 200 || json == null) {
        throw ChatUnavailableException(
          'unexpected canned response',
          statusCode: res.statusCode,
        );
      }
      return CannedCatalog.fromJson(json);
    } on DioException catch (e) {
      throw ChatUnavailableException('canned fetch failed',
          statusCode: e.response?.statusCode);
    }
  }
}
