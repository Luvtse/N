import 'dart:async';
import 'package:flutter/foundation.dart';
import 'package:firebase_analytics/firebase_analytics.dart';

import '../network/api_client.dart';

/// Analytics service for tracking user behavior and events
class AnalyticsService {
  final FirebaseAnalytics _firebaseAnalytics;
  final ApiClient? _apiClient;
  
  bool _isEnabled = true;
  final Map<String, dynamic> _userProperties = {};
  final List<AnalyticsEvent> _eventQueue = [];
  Timer? _flushTimer;

  AnalyticsService({
    required FirebaseAnalytics firebaseAnalytics,
    ApiClient? apiClient,
  })  : _firebaseAnalytics = firebaseAnalytics,
        _apiClient = apiClient {
    _initializeFlushTimer();
  }

  // ============================================================================
  // INITIALIZATION
  // ============================================================================

  void _initializeFlushTimer() {
    _flushTimer = Timer.periodic(
      const Duration(minutes: 5),
      (_) => _flushEventQueue(),
    );
  }

  /// Enable or disable analytics
  Future<void> setEnabled(bool enabled) async {
    _isEnabled = enabled;
    await _firebaseAnalytics.setAnalyticsCollectionEnabled(enabled);
  }

  /// Set user ID for analytics
  Future<void> setUserId(String? userId) async {
    await _firebaseAnalytics.setUserId(userId: userId);
    if (userId != null) {
      _userProperties['user_id'] = userId;
    }
  }

  /// Set user property
  Future<void> setUserProperty(String name, String? value) async {
    await _firebaseAnalytics.setUserProperty(name: name, value: value);
    if (value != null) {
      _userProperties[name] = value;
    }
  }

  // ============================================================================
  // AUTHENTICATION EVENTS
  // ============================================================================

  Future<void> trackLogin(String method) async {
    await _trackEvent(AnalyticsEvent(
      name: 'login',
      parameters: {'method': method},
    ));
  }

  Future<void> trackSignUp(String method) async {
    await _trackEvent(AnalyticsEvent(
      name: 'sign_up',
      parameters: {'method': method},
    ));
  }

  Future<void> trackLogout() async {
    await _trackEvent(const AnalyticsEvent(name: 'logout'));
  }

  // ============================================================================
  // RIDE EVENTS
  // ============================================================================

  Future<void> trackRideRequested({
    required String rideId,
    required String rideType,
    required double estimatedFare,
    required double distanceKm,
  }) async {
    await _trackEvent(AnalyticsEvent(
      name: 'ride_requested',
      parameters: {
        'ride_id': rideId,
        'ride_type': rideType,
        'estimated_fare': estimatedFare,
        'distance_km': distanceKm,
      },
    ));
  }

  Future<void> trackRideMatched({
    required String rideId,
    required String driverId,
    required int etaMinutes,
  }) async {
    await _trackEvent(AnalyticsEvent(
      name: 'ride_matched',
      parameters: {
        'ride_id': rideId,
        'driver_id': driverId,
        'eta_minutes': etaMinutes,
      },
    ));
  }

  Future<void> trackRideCompleted({
    required String rideId,
    required double fare,
    required double rating,
    required double tip,
  }) async {
    await _trackEvent(AnalyticsEvent(
      name: 'ride_completed',
      parameters: {
        'ride_id': rideId,
        'fare': fare,
        'rating': rating,
        'tip': tip,
      },
    ));
  }

  Future<void> trackRideCancelled({
    required String rideId,
    required String reason,
    required String cancelledBy,
  }) async {
    await _trackEvent(AnalyticsEvent(
      name: 'ride_cancelled',
      parameters: {
        'ride_id': rideId,
        'reason': reason,
        'cancelled_by': cancelledBy,
      },
    ));
  }

  // ============================================================================
  // HOTEL EVENTS
  // ============================================================================

  Future<void> trackHotelSearch({
    required String city,
    required DateTime checkIn,
    required DateTime checkOut,
    required int guests,
  }) async {
    await _trackEvent(AnalyticsEvent(
      name: 'hotel_search',
      parameters: {
        'city': city,
        'check_in': checkIn.toIso8601String(),
        'check_out': checkOut.toIso8601String(),
        'guests': guests,
      },
    ));
  }

  Future<void> trackHotelBooked({
    required String bookingId,
    required String hotelId,
    required double amount,
    required int nights,
  }) async {
    await _trackEvent(AnalyticsEvent(
      name: 'hotel_booked',
      parameters: {
        'booking_id': bookingId,
        'hotel_id': hotelId,
        'amount': amount,
        'nights': nights,
      },
    ));
  }

  // ============================================================================
  // FOOD EVENTS
  // ============================================================================

  Future<void> trackRestaurantViewed(String restaurantId) async {
    await _trackEvent(AnalyticsEvent(
      name: 'restaurant_viewed',
      parameters: {'restaurant_id': restaurantId},
    ));
  }

  Future<void> trackOrderPlaced({
    required String orderId,
    required String restaurantId,
    required double amount,
    required int itemCount,
  }) async {
    await _trackEvent(AnalyticsEvent(
      name: 'order_placed',
      parameters: {
        'order_id': orderId,
        'restaurant_id': restaurantId,
        'amount': amount,
        'item_count': itemCount,
      },
    ));
  }

  // ============================================================================
  // SCREEN TRACKING
  // ============================================================================

  Future<void> trackScreenView(String screenName) async {
    await _firebaseAnalytics.logScreenView(screenName: screenName);
    await _trackEvent(AnalyticsEvent(
      name: 'screen_view',
      parameters: {'screen_name': screenName},
    ));
  }

  // ============================================================================
  // CUSTOM EVENTS
  // ============================================================================

  Future<void> trackCustomEvent(
    String name, {
    Map<String, dynamic>? parameters,
  }) async {
    await _trackEvent(AnalyticsEvent(
      name: name,
      parameters: parameters,
    ));
  }

  // ============================================================================
  // INTERNAL METHODS
  // ============================================================================

  Future<void> _trackEvent(AnalyticsEvent event) async {
    if (!_isEnabled) return;

    // Add to queue
    _eventQueue.add(event);

    // Send to Firebase Analytics
    try {
      await _firebaseAnalytics.logEvent(
        name: event.name,
        parameters: event.parameters,
      );
    } catch (e) {
      debugPrint('Failed to track event to Firebase: $e');
    }

    // Flush queue if it's getting large
    if (_eventQueue.length >= 50) {
      await _flushEventQueue();
    }
  }

  Future<void> _flushEventQueue() async {
    if (_eventQueue.isEmpty || _apiClient == null) return;

    final events = List<AnalyticsEvent>.from(_eventQueue);
    _eventQueue.clear();

    try {
      await _apiClient!.post(
        '/api/v1/analytics/events',
        data: {
          'events': events.map((e) => e.toJson()).toList(),
          'user_properties': _userProperties,
          'timestamp': DateTime.now().toIso8601String(),
        },
      );
    } catch (e) {
      // Re-queue failed events (with limit to prevent memory issues)
      if (_eventQueue.length < 100) {
        _eventQueue.insertAll(0, events);
      }
      debugPrint('Failed to flush events to backend: $e');
    }
  }

  /// Dispose resources
  void dispose() {
    _flushTimer?.cancel();
    _flushEventQueue();
  }
}

/// Analytics event model
class AnalyticsEvent {
  final String name;
  final Map<String, dynamic>? parameters;
  final DateTime timestamp;

  AnalyticsEvent({
    required this.name,
    this.parameters,
    DateTime? timestamp,
  }) : timestamp = timestamp ?? DateTime.now();

  Map<String, dynamic> toJson() {
    return {
      'name': name,
      'parameters': parameters ?? {},
      'timestamp': timestamp.toIso8601String(),
    };
  }
}