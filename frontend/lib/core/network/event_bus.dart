import 'dart:async';

/// Event bus for cross-feature communication
/// 
/// This allows different features to communicate without direct dependencies.
/// Example: Auth feature emits "user_logged_in" event, Ride feature listens
/// and updates its state accordingly.
class EventBus {
  final StreamController<dynamic> _controller = StreamController.broadcast();
  
  Stream<dynamic> get stream => _controller.stream;

  /// Emit an event to all subscribers
  void emit(dynamic event) {
    _controller.add(event);
  }

  /// Subscribe to events of a specific type
  Stream<T> on<T>() {
    return stream.where((event) => event is T).cast<T>();
  }

  /// Subscribe to all events
  Stream<dynamic> onAll() {
    return stream;
  }

  void dispose() {
    _controller.close();
  }
}

// ============================================================================
// COMMON EVENTS
// ============================================================================

/// User authentication events
class UserLoggedIn {
  final String userId;
  UserLoggedIn(this.userId);
}

class UserLoggedOut {}

class UserProfileUpdated {
  final Map<String, dynamic> profile;
  UserProfileUpdated(this.profile);
}

/// Ride events
class RideRequested {
  final String rideId;
  RideRequested(this.rideId);
}

class RideStatusChanged {
  final String rideId;
  final String status;
  RideStatusChanged(this.rideId, this.status);
}

class RideCompleted {
  final String rideId;
  RideCompleted(this.rideId);
}

/// Network events
class NetworkStatusChanged {
  final bool isOnline;
  NetworkStatusChanged(this.isOnline);
}

/// Notification events
class NotificationReceived {
  final String title;
  final String body;
  final Map<String, dynamic>? data;
  NotificationReceived(this.title, this.body, this.data);
}

/// Location events
class LocationUpdated {
  final double latitude;
  final double longitude;
  LocationUpdated(this.latitude, this.longitude);
}

/// Theme events
class ThemeChanged {
  final bool isDarkMode;
  ThemeChanged(this.isDarkMode);
}

/// Language events
class LanguageChanged {
  final String languageCode;
  LanguageChanged(this.languageCode);
}