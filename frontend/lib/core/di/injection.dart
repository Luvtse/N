import 'package:flutter/foundation.dart';
import 'package:get_it/get_it.dart';
import 'package:connectivity_plus/connectivity_plus.dart';
import 'package:hive_flutter/hive_flutter.dart';

import '../network/api_client.dart';
import '../network/websocket_client.dart';
import '../network/event_bus.dart';
import '../offline/offline_service.dart';
import '../router/app_router.dart';
import '../../features/auth/data/repositories/auth_repository.dart';
import '../../features/auth/domain/usecases/login_usecase.dart';
import '../../features/auth/domain/usecases/register_usecase.dart';
import '../../features/auth/presentation/bloc/auth_bloc.dart';
import '../../features/auth/presentation/bloc/legal_consent_bloc.dart';
import '../../features/nidus/data/repositories/ride_repository.dart';
import '../../features/nidus/data/repositories/safety_repository.dart';
import '../../features/nidus/domain/usecases/request_ride_usecase.dart';
import '../../features/nidus/domain/usecases/get_ride_status_usecase.dart';
import '../../features/nidus/presentation/bloc/ride_bloc.dart';
import '../../features/haven/data/repositories/hotel_repository.dart';
import '../../features/haven/presentation/bloc/hotel_search_bloc.dart';
import '../../features/vorax/data/repositories/restaurant_repository.dart';
import '../../features/vorax/presentation/bloc/restaurant_bloc.dart';
import '../../features/profile/data/repositories/user_repository.dart';
import '../../features/profile/presentation/bloc/profile_bloc.dart';
import '../../features/wallet/data/repositories/wallet_repository.dart';
import '../../features/wallet/presentation/bloc/wallet_bloc.dart';

// ============================================================================
// SERVICE LOCATOR
// ============================================================================

final GetIt getIt = GetIt.instance;

// ============================================================================
// INITIALIZATION
// ============================================================================

/// Initialize all dependencies. Call this once in main() before runApp().
Future<void> configureDependencies() async {
  // 1. Initialize Hive for local storage
  await Hive.initFlutter();
  await Hive.openBox('auth');
  await Hive.openBox('cache');
  await Hive.openBox('settings');

  // 2. Register core services (singletons)
  _registerCoreServices();

  // 3. Register network layer
  _registerNetworkLayer();

  // 4. Register repositories
  _registerRepositories();

  // 5. Register use cases
  _registerUseCases();

  // 6. Register BLoCs (factories - new instance per screen)
  _registerBlocs();

  // 7. Register router
  _registerRouter();

  debugPrint('✅ All dependencies configured successfully');
}

// ============================================================================
// CORE SERVICES
// ============================================================================

void _registerCoreServices() {
  // Connectivity
  getIt.registerLazySingleton<Connectivity>(() => Connectivity());

  // Event Bus (cross-feature communication)
  getIt.registerLazySingleton<EventBus>(() => EventBus());

  // Offline Service
  getIt.registerLazySingleton<OfflineService>(() => OfflineService());

  // Token Storage (using Hive)
  getIt.registerLazySingleton<TokenStorage>(() => HiveTokenStorage());
}

// ============================================================================
// NETWORK LAYER
// ============================================================================

void _registerNetworkLayer() {
  // API Client Configuration
  const apiConfig = ApiClientConfig(
    baseUrl: String.fromEnvironment(
      'API_BASE_URL',
      defaultValue: 'http://localhost:8080',
    ),
    connectTimeout: Duration(seconds: 15),
    receiveTimeout: Duration(seconds: 30),
    sendTimeout: Duration(seconds: 15),
    enableLogging: kDebugMode,
    maxRetries: 3,
  );

  // API Client (singleton - shared across app)
  getIt.registerLazySingleton<ApiClient>(
    () => ApiClient(
      config: apiConfig,
      tokenStorage: getIt<TokenStorage>(),
    ),
  );

  // WebSocket Client (singleton)
  getIt.registerLazySingleton<WebSocketClient>(
    () => WebSocketClient(
      baseUrl: String.fromEnvironment(
        'WS_BASE_URL',
        defaultValue: 'ws://localhost:8080/ws',
      ),
      eventBus: getIt<EventBus>(),
    ),
  );
}

// ============================================================================
// REPOSITORIES
// ============================================================================

void _registerRepositories() {
  // Auth Repository
  getIt.registerLazySingleton<AuthRepository>(
    () => AuthRepositoryImpl(
      apiClient: getIt<ApiClient>(),
      tokenStorage: getIt<TokenStorage>(),
    ),
  );

  // User Repository
  getIt.registerLazySingleton<UserRepository>(
    () => UserRepositoryImpl(apiClient: getIt<ApiClient>()),
  );

  // Ride Repository
  getIt.registerLazySingleton<RideRepository>(
    () => RideRepositoryImpl(
      apiClient: getIt<ApiClient>(),
      webSocketClient: getIt<WebSocketClient>(),
    ),
  );

  // Safety Repository (SOS, trip sharing, trusted contacts)
  getIt.registerLazySingleton<SafetyRepository>(
    () => SafetyRepositoryImpl(
      apiClient: getIt<ApiClient>(),
      webSocketClient: getIt<WebSocketClient>(),
    ),
  );

  // Hotel Repository
  getIt.registerLazySingleton<HotelRepository>(
    () => HotelRepositoryImpl(apiClient: getIt<ApiClient>()),
  );

  // Restaurant Repository
  getIt.registerLazySingleton<RestaurantRepository>(
    () => RestaurantRepositoryImpl(apiClient: getIt<ApiClient>()),
  );

  // Wallet (Ledger) Repository — Phase H Step 1
  getIt.registerLazySingleton<WalletRepository>(
    () => WalletRepositoryImpl(apiClient: getIt<ApiClient>()),
  );
}

// ============================================================================
// USE CASES
// ============================================================================

void _registerUseCases() {
  // Auth
  getIt.registerFactory(() => LoginUseCase(getIt<AuthRepository>()));
  getIt.registerFactory(() => RegisterUseCase(getIt<AuthRepository>()));

  // Rides
  getIt.registerFactory(() => RequestRideUseCase(getIt<RideRepository>()));
  getIt.registerFactory(() => GetRideStatusUseCase(getIt<RideRepository>()));
}

// ============================================================================
// BLOCS
// ============================================================================

void _registerBlocs() {
  // Auth BLoC (singleton - manages global auth state)
  getIt.registerLazySingleton<AuthBloc>(
    () => AuthBloc(
      loginUseCase: getIt<LoginUseCase>(),
      registerUseCase: getIt<RegisterUseCase>(),
      authRepository: getIt<AuthRepository>(),
    ),
  );

  // Legal Consent BLoC (singleton - tracks consent across app)
  getIt.registerLazySingleton<LegalConsentBloc>(
    () => LegalConsentBloc(getIt<ApiClient>()),
  );

  // Ride BLoC (factory - new instance per ride screen)
  getIt.registerFactory<RideBloc>(
    () => RideBloc(
      rideRepository: getIt<RideRepository>(),
      requestRideUseCase: getIt<RequestRideUseCase>(),
      getRideStatusUseCase: getIt<GetRideStatusUseCase>(),
      safetyRepository: getIt<SafetyRepository>(),
    ),
  );

  // Hotel Search BLoC (factory)
  getIt.registerFactory<HotelSearchBloc>(
    () => HotelSearchBloc(hotelRepository: getIt<HotelRepository>()),
  );

  // Restaurant BLoC (factory)
  getIt.registerFactory<RestaurantBloc>(
    () => RestaurantBloc(restaurantRepository: getIt<RestaurantRepository>()),
  );

  // Profile BLoC (singleton - global user state)
  getIt.registerLazySingleton<ProfileBloc>(
    () => ProfileBloc(userRepository: getIt<UserRepository>()),
  );

  // Wallet BLoC (factory - new instance per wallet screen)
  getIt.registerFactory<WalletBloc>(
    () => WalletBloc(repository: getIt<WalletRepository>()),
  );
}

// ============================================================================
// ROUTER
// ============================================================================

void _registerRouter() {
  getIt.registerLazySingleton<AppRouter>(
    () => AppRouter(
      authBloc: getIt<AuthBloc>(),
      rideBlocFactory: getIt<RideBloc>,
      walletBlocFactory: getIt<WalletBloc>,
    ),
  );
}

// ============================================================================
// TOKEN STORAGE IMPLEMENTATION
// ============================================================================

/// Token storage using Hive (encrypted in production)
class HiveTokenStorage implements TokenStorage {
  static const _accessTokenKey = 'access_token';
  static const _refreshTokenKey = 'refresh_token';

  late final _authBox = Hive.box('auth');

  @override
  Future<String?> getAccessToken() async {
    return _authBox.get(_accessTokenKey) as String?;
  }

  @override
  Future<String?> getRefreshToken() async {
    return _authBox.get(_refreshTokenKey) as String?;
  }

  @override
  Future<void> saveTokens({
    required String accessToken,
    required String refreshToken,
  }) async {
    await _authBox.put(_accessTokenKey, accessToken);
    await _authBox.put(_refreshTokenKey, refreshToken);
  }

  @override
  Future<void> clearTokens() async {
    await _authBox.delete(_accessTokenKey);
    await _authBox.delete(_refreshTokenKey);
  }
}

// ============================================================================
// CLEANUP
// ============================================================================

/// Reset all dependencies (useful for testing)
Future<void> resetDependencies() async {
  await getIt.reset();
}

/// Dispose singleton resources on app shutdown
Future<void> disposeDependencies() async {
  if (getIt.isRegistered<ApiClient>()) {
    getIt<ApiClient>().dispose();
  }
  if (getIt.isRegistered<WebSocketClient>()) {
    getIt<WebSocketClient>().dispose();
  }
  if (getIt.isRegistered<EventBus>()) {
    getIt<EventBus>().dispose();
  }
  if (getIt.isRegistered<OfflineService>()) {
    getIt<OfflineService>().dispose();
  }
}