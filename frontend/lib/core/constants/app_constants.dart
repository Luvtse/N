/// Application-wide constants for NIDAW
class AppConstants {
  AppConstants._();

  // ============================================================================
  // API CONFIGURATION
  // ============================================================================

  /// Base URL for API requests
  static const String apiBaseUrl = String.fromEnvironment(
    'API_BASE_URL',
    defaultValue: 'http://localhost:8080',
  );

  /// WebSocket URL for real-time communication
  static const String wsBaseUrl = String.fromEnvironment(
    'WS_BASE_URL',
    defaultValue: 'ws://localhost:8080/ws',
  );

  /// API version
  static const String apiVersion = 'v1';

  /// API endpoints
  static const String authEndpoint = '/api/$apiVersion/auth';
  static const String ridesEndpoint = '/api/$apiVersion/nidus/rides';
  static const String driversEndpoint = '/api/$apiVersion/nidus/drivers';
  static const String hotelsEndpoint = '/api/$apiVersion/haven/hotels';
  static const String bookingsEndpoint = '/api/$apiVersion/haven/bookings';
  static const String restaurantsEndpoint = '/api/$apiVersion/vorax/restaurants';
  static const String ordersEndpoint = '/api/$apiVersion/vorax/orders';

  // ============================================================================
  // TIMEOUTS
  // ============================================================================

  /// Connection timeout in seconds
  static const int connectionTimeout = 15;

  /// Receive timeout in seconds
  static const int receiveTimeout = 30;

  /// Send timeout in seconds
  static const int sendTimeout = 15;

  /// WebSocket heartbeat interval in seconds
  static const int wsHeartbeatInterval = 30;

  /// WebSocket reconnect delay in seconds
  static const int wsReconnectDelay = 5;

  // ============================================================================
  // STORAGE KEYS
  // ============================================================================

  /// Hive box names
  static const String authBox = 'auth';
  static const String cacheBox = 'cache';
  static const String settingsBox = 'settings';

  /// Secure storage keys
  static const String accessTokenKey = 'access_token';
  static const String refreshTokenKey = 'refresh_token';
  static const String tokenExpiresAtKey = 'token_expires_at';
  static const String userIdKey = 'user_id';
  static const String userRoleKey = 'user_role';

  // ============================================================================
  // PAGINATION
  // ============================================================================

  /// Default page size
  static const int defaultPageSize = 20;

  /// Maximum page size
  static const int maxPageSize = 100;

  // ============================================================================
  // LOCATION
  // ============================================================================

  /// Default map zoom level
  static const double defaultMapZoom = 14.0;

  /// Location update distance filter in meters
  static const int locationDistanceFilter = 10;

  /// Location accuracy
  static const String locationAccuracy = 'high';

  // ============================================================================
  // RIDE TYPES
  // ============================================================================

  /// Available ride types
  static const List<String> rideTypes = [
    'standard',
    'premium',
    'electric',
    'shared',
    'wheelchair',
  ];

  /// Ride type labels
  static const Map<String, String> rideTypeLabels = {
    'standard': 'Standard',
    'premium': 'Premium',
    'electric': 'Electric',
    'shared': 'Shared',
    'wheelchair': 'Wheelchair Accessible',
  };

  // ============================================================================
  // USER ROLES
  // ============================================================================

  /// User roles
  static const String roleRider = 'rider';
  static const String roleDriver = 'driver';
  static const String roleAdmin = 'admin';
  static const String roleCorporate = 'corporate';

  // ============================================================================
  // RIDE STATUS
  // ============================================================================

  /// Ride status values
  static const String rideStatusRequested = 'requested';
  static const String rideStatusSearching = 'searching';
  static const String rideStatusMatched = 'matched';
  static const String rideStatusDriverEnRoute = 'driver_en_route';
  static const String rideStatusInProgress = 'in_progress';
  static const String rideStatusCompleted = 'completed';
  static const String rideStatusCancelled = 'cancelled';

  // ============================================================================
  // DRIVER STATUS
  // ============================================================================

  /// Driver status values
  static const String driverStatusOffline = 'offline';
  static const String driverStatusAvailable = 'available';
  static const String driverStatusBusy = 'busy';
  static const String driverStatusOnTrip = 'on_trip';

  // ============================================================================
  // CURRENCY
  // ============================================================================

  /// Default currency
  static const String defaultCurrency = 'USD';

  /// Supported currencies
  static const List<String> supportedCurrencies = [
    'USD',
    'EUR',
    'GBP',
    'JPY',
    'CAD',
    'AUD',
  ];

  // ============================================================================
  // ERROR MESSAGES
  // ============================================================================

  /// Network error messages
  static const String networkError = 'Network error. Please check your connection.';
  static const String timeoutError = 'Request timeout. Please try again.';
  static const String serverError = 'Server error. Please try again later.';
  static const String unknownError = 'An unknown error occurred.';

  /// Validation error messages
  static const String invalidEmail = 'Please enter a valid email address.';
  static const String invalidPassword = 'Password must be at least 8 characters.';
  static const String invalidPhone = 'Please enter a valid phone number.';
  static const String requiredField = 'This field is required.';

  // ============================================================================
  // SUCCESS MESSAGES
  // ============================================================================

  static const String loginSuccess = 'Login successful!';
  static const String logoutSuccess = 'Logout successful!';
  static const String registrationSuccess = 'Registration successful!';
  static const String profileUpdated = 'Profile updated successfully!';
  static const String rideRequested = 'Ride requested successfully!';
  static const String rideCompleted = 'Ride completed successfully!';

  // ============================================================================
  // IMAGE ASSETS
  // ============================================================================

  static const String logoPath = 'assets/images/logo.png';
  static const String placeholderImage = 'assets/images/placeholder.png';
  static const String defaultAvatar = 'assets/images/default_avatar.png';

  // ============================================================================
  // FEATURE FLAGS
  // ============================================================================

  static const bool enableAutonomousVehicles = bool.fromEnvironment(
    'FEATURE_AUTONOMOUS_VEHICLES',
    defaultValue: false,
  );

  static const bool enableDroneDelivery = bool.fromEnvironment(
    'FEATURE_DRONE_DELIVERY',
    defaultValue: false,
  );

  // Phase C: blockchain payments decommissioned. Replaced by the private
  // in-app currency ledger (Phase D). Off by default until ledger ships.
  static const bool enableInternalLedger = bool.fromEnvironment(
    'FEATURE_INTERNAL_LEDGER',
    defaultValue: false,
  );

  static const bool enableARNavigation = bool.fromEnvironment(
    'FEATURE_AR_NAVIGATION',
    defaultValue: true,
  );

  static const bool enableVoiceCommands = bool.fromEnvironment(
    'FEATURE_VOICE_COMMANDS',
    defaultValue: true,
  );
}