import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';

import '../../../../core/network/api_client.dart';
import '../../../../core/network/websocket_client.dart';

// ============================================================================
// ABSTRACT INTERFACE
// ============================================================================

/// Repository for rider safety features: SOS, trip sharing, and trusted
/// contacts. Backed by the `/api/v1/nidus/safety` endpoint group.
abstract class SafetyRepository {
  /// Trigger an emergency SOS for a ride. Returns the incident id.
  Future<SosResult> triggerSos({
    required String rideId,
    required double lat,
    required double lng,
    String? message,
  });

  /// Create a tokenized live-tracking link for an active ride.
  Future<ShareLinkResult> createShareLink({
    required String rideId,
    required String label,
  });

  /// List share links that are still active for [rideId].
  Future<List<SharedTripLink>> listActiveShareLinks(String rideId);

  /// Revoke a previously created share link.
  Future<bool> revokeShareLink(String linkId);

  /// Trusted contacts (auto-notified on SOS / long-trip check-ins).
  Future<List<TrustedContact>> listTrustedContacts();

  Future<TrustedContact> addTrustedContact({
    required String name,
    required String phone,
    bool autoNotify = true,
  });

  Future<bool> removeTrustedContact(String contactId);

  Future<bool> updateTrustedContact(
    String contactId, {
    String? name,
    String? phone,
    bool? autoNotify,
  });

  /// Subscribe to live location updates for a shared trip topic.
  Stream<SharedTripLocation> subscribeToSharedTrip(String linkToken);

  /// Unsubscribe from a shared trip topic.
  void unsubscribeFromSharedTrip(String linkToken);
}

// ============================================================================
// DOMAIN ENTITIES
// ============================================================================

/// Result of an SOS request.
class SosResult {
  final bool success;
  final String? incidentId;
  final String? errorCode;
  final String? message;

  const SosResult._({
    required this.success,
    this.incidentId,
    this.errorCode,
    this.message,
  });

  const SosResult.triggered({required this.incidentId})
      : success = true,
        errorCode = null,
        message = null;

  const SosResult.failure({
    required this.errorCode,
    required this.message,
  })  : success = false,
        incidentId = null;
}

/// Result of creating a share link.
class ShareLinkResult {
  final bool success;
  final SharedTripLink? link;
  final String? errorCode;
  final String? message;

  const ShareLinkResult._({
    required this.success,
    this.link,
    this.errorCode,
    this.message,
  });

  const ShareLinkResult.created({required this.link})
      : success = true,
        errorCode = null,
        message = null;

  const ShareLinkResult.failure({
    required this.errorCode,
    required this.message,
  })  : success = false,
        link = null;
}

/// A tokenized public tracking link for one ride.
class SharedTripLink {
  final String id;
  final String rideId;
  final String label;
  final String token;
  final DateTime createdAt;
  final DateTime? expiresAt;

  const SharedTripLink({
    required this.id,
    required this.rideId,
    required this.label,
    required this.token,
    required this.createdAt,
    this.expiresAt,
  });

  factory SharedTripLink.fromJson(Map<String, dynamic> json) {
    return SharedTripLink(
      id: json['id'] as String,
      rideId: json['ride_id'] as String,
      label: json['label'] as String? ?? 'My trip',
      token: json['token'] as String,
      createdAt: DateTime.parse(json['created_at'] as String),
      expiresAt: json['expires_at'] != null
          ? DateTime.parse(json['expires_at'] as String)
          : null,
    );
  }

  /// Public web URL a contact can open without the app installed.
  String get publicUrl => 'https://nidus.app/track/$token';
}

/// A person the rider trusts with live trip data.
class TrustedContact {
  final String id;
  final String name;
  final String phone;
  final bool autoNotify;

  const TrustedContact({
    required this.id,
    required this.name,
    required this.phone,
    this.autoNotify = true,
  });

  factory TrustedContact.fromJson(Map<String, dynamic> json) {
    return TrustedContact(
      id: json['id'] as String,
      name: json['name'] as String,
      phone: json['phone'] as String,
      autoNotify: json['auto_notify'] as bool? ?? true,
    );
  }

  Map<String, dynamic> toJson() => {
        'name': name,
        'phone': phone,
        'auto_notify': autoNotify,
      };
}

/// One position sample streamed to viewers of a shared trip.
class SharedTripLocation {
  final double lat;
  final double lng;
  final String rideStatus;
  final DateTime timestamp;

  const SharedTripLocation({
    required this.lat,
    required this.lng,
    required this.rideStatus,
    required this.timestamp,
  });

  factory SharedTripLocation.fromJson(Map<String, dynamic> json) {
    return SharedTripLocation(
      lat: (json['lat'] as num).toDouble(),
      lng: (json['lng'] as num).toDouble(),
      rideStatus: json['status'] as String? ?? 'unknown',
      timestamp: DateTime.tryParse(json['timestamp'] as String? ?? '') ??
          DateTime.now(),
    );
  }
}

// ============================================================================
// IMPLEMENTATION
// ============================================================================

class SafetyRepositoryImpl implements SafetyRepository {
  static const String _base = '/api/v1/nidus/safety';

  final ApiClient _apiClient;
  final WebSocketClient _webSocketClient;

  final Map<String, StreamController<SharedTripLocation>> _subscriptions = {};

  SafetyRepositoryImpl({
    required ApiClient apiClient,
    required WebSocketClient webSocketClient,
  })  : _apiClient = apiClient,
        _webSocketClient = webSocketClient;

  @override
  Future<SosResult> triggerSos({
    required String rideId,
    required double lat,
    required double lng,
    String? message,
  }) async {
    try {
      final response = await _apiClient.post<Map<String, dynamic>>(
        '$_base/sos',
        data: {
          'ride_id': rideId,
          'lat': lat,
          'lng': lng,
          'message': message,
        },
      );

      final data = response.data;
      return SosResult.triggered(incidentId: data?['incident_id'] as String?);
    } on ApiException catch (e) {
      return SosResult.failure(
        errorCode: e.errorCode ?? 'SOS_FAILED',
        message: e.message,
      );
    } catch (e) {
      return SosResult.failure(
        errorCode: 'NETWORK_ERROR',
        message: 'Failed to trigger SOS: ${e.toString()}',
      );
    }
  }

  @override
  Future<ShareLinkResult> createShareLink({
    required String rideId,
    required String label,
  }) async {
    try {
      final response = await _apiClient.post<Map<String, dynamic>>(
        '$_base/share-links',
        data: {'ride_id': rideId, 'label': label},
      );

      final data = response.data;
      if (data == null) {
        return const ShareLinkResult.failure(
          errorCode: 'EMPTY_RESPONSE',
          message: 'Server returned no share link',
        );
      }
      return ShareLinkResult.created(link: SharedTripLink.fromJson(data));
    } on ApiException catch (e) {
      return ShareLinkResult.failure(
        errorCode: e.errorCode ?? 'SHARE_FAILED',
        message: e.message,
      );
    } catch (e) {
      return ShareLinkResult.failure(
        errorCode: 'NETWORK_ERROR',
        message: 'Failed to create share link: ${e.toString()}',
      );
    }
  }

  @override
  Future<List<SharedTripLink>> listActiveShareLinks(String rideId) async {
    try {
      final response = await _apiClient.get<Map<String, dynamic>>(
        '$_base/share-links',
        queryParameters: {'ride_id': rideId},
      );

      final data = response.data;
      final links = data?['links'];
      if (links is List) {
        return links
            .map((l) => SharedTripLink.fromJson(l as Map<String, dynamic>))
            .toList();
      }
      return const [];
    } catch (e) {
      debugPrint('Failed to list share links: $e');
      return const [];
    }
  }

  @override
  Future<bool> revokeShareLink(String linkId) async {
    try {
      await _apiClient.delete('$_base/share-links/$linkId');
      return true;
    } catch (e) {
      debugPrint('Failed to revoke share link: $e');
      return false;
    }
  }

  @override
  Future<List<TrustedContact>> listTrustedContacts() async {
    try {
      final response = await _apiClient.get<Map<String, dynamic>>(
        '$_base/contacts',
      );

      final data = response.data;
      final contacts = data?['contacts'];
      if (contacts is List) {
        return contacts
            .map((c) => TrustedContact.fromJson(c as Map<String, dynamic>))
            .toList();
      }
      return const [];
    } catch (e) {
      debugPrint('Failed to list trusted contacts: $e');
      return const [];
    }
  }

  @override
  Future<TrustedContact> addTrustedContact({
    required String name,
    required String phone,
    bool autoNotify = true,
  }) async {
    final response = await _apiClient.post<Map<String, dynamic>>(
      '$_base/contacts',
      data: {
        'name': name,
        'phone': phone,
        'auto_notify': autoNotify,
      },
    );

    final data = response.data;
    if (data == null) {
      throw const SosFailure('EMPTY_RESPONSE', 'Server returned no contact');
    }
    return TrustedContact.fromJson(data);
  }

  @override
  Future<bool> removeTrustedContact(String contactId) async {
    try {
      await _apiClient.delete('$_base/contacts/$contactId');
      return true;
    } catch (e) {
      debugPrint('Failed to remove trusted contact: $e');
      return false;
    }
  }

  @override
  Future<bool> updateTrustedContact(
    String contactId, {
    String? name,
    String? phone,
    bool? autoNotify,
  }) async {
    try {
      final body = <String, dynamic>{};
      if (name != null) body['name'] = name;
      if (phone != null) body['phone'] = phone;
      if (autoNotify != null) body['auto_notify'] = autoNotify;

      await _apiClient.patch('$_base/contacts/$contactId', data: body);
      return true;
    } catch (e) {
      debugPrint('Failed to update trusted contact: $e');
      return false;
    }
  }

  // ==========================================================================
  // WEBSOCKET SUBSCRIPTIONS
  // ==========================================================================

  @override
  Stream<SharedTripLocation> subscribeToSharedTrip(String linkToken) {
    if (_subscriptions.containsKey(linkToken)) {
      return _subscriptions[linkToken]!.stream;
    }

    final controller = StreamController<SharedTripLocation>.broadcast();
    _subscriptions[linkToken] = controller;

    _webSocketClient.subscribe('shared_trip:$linkToken', (message) {
      try {
        final decoded = _decodeMessage(message);
        if (decoded != null) {
          controller.add(SharedTripLocation.fromJson(decoded));
        }
      } catch (e) {
        debugPrint('Failed to parse shared trip update: $e');
      }
    });

    return controller.stream;
  }

  @override
  void unsubscribeFromSharedTrip(String linkToken) {
    final controller = _subscriptions.remove(linkToken);
    if (controller != null) {
      controller.close();
      _webSocketClient.unsubscribe('shared_trip:$linkToken');
    }
  }

  Map<String, dynamic>? _decodeMessage(dynamic message) {
    if (message is Map<String, dynamic>) return message;
    if (message is String) {
      try {
        final decoded = jsonDecode(message);
        if (decoded is Map<String, dynamic>) return decoded;
      } catch (e) {
        debugPrint('Shared trip payload not valid JSON: $e');
      }
    }
    return null;
  }

  // ==========================================================================
  // CLEANUP
  // ==========================================================================

  void dispose() {
    for (final controller in _subscriptions.values) {
      controller.close();
    }
    _subscriptions.clear();
  }
}

/// Thrown by [SafetyRepositoryImpl.addTrustedContact] on unrecoverable errors.
class SosFailure implements Exception {
  final String code;
  final String message;

  const SosFailure(this.code, this.message);

  @override
  String toString() => 'SosFailure($code): $message';
}
