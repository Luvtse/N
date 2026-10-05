import 'package:get_it/get_it.dart';
import 'package:hive/hive.dart';

import '../network/api_client.dart';
import '../network/websocket_client.dart';
import '../network/location_service.dart';
import '../router/app_router.dart';

import '../../features/auth/data/repositories/auth_repository.dart';
import '../../features/auth/domain/usecases/login_usecase.dart';
import '../../features/auth/domain/usecases/register_usecase.dart';
import '../../features/auth/presentation/bloc/auth_bloc.dart';

import '../../features/home/data/repositories/driver_home_repository.dart';
import '../../features/home/domain/usecases/get_driver_stats_usecase.dart';
import '../../features/home/domain/usecases/toggle_availability_usecase.dart';
import '../../features/home/presentation/bloc/driver_home_bloc.dart';

import '../../features/ride/data/repositories/ride_repository.dart';
import '../../features/ride/domain/usecases/accept_ride_usecase.dart';
import '../../features/ride/domain/usecases/start_ride_usecase.dart';
import '../../features/ride/domain/usecases/complete_ride_usecase.dart';
import '../../features/ride/presentation/bloc/active_ride_bloc.dart';

import '../../features/earnings/data/repositories/earnings_repository.dart';
import '../../features/earnings/domain/usecases/get_daily_earnings_usecase.dart';
import '../../features/earnings/domain/usecases/get_weekly_earnings_usecase.dart';
import '../../features/earnings/presentation/bloc/earnings_bloc.dart';

import '../../features/profile/data/repositories/driver_profile_repository.dart';
import '../../features/profile/presentation/bloc/driver_profile_bloc.dart';

final getIt = GetIt.instance;

Future<void> configureDependencies() async {
  // Network
  getIt.registerLazySingleton<ApiClient>(
    () => ApiClient(baseUrl: const String.fromEnvironment(
      'API_BASE_URL',
      defaultValue: 'http://localhost:8080',
    )),
  );

  getIt.registerLazySingleton<WebSocketClient>(
    () => WebSocketClient(baseUrl: const String.fromEnvironment(
      'API_BASE_URL',
      defaultValue: 'http://localhost:8080',
    )),
  );

  getIt.registerLazySingleton<LocationService>(
    () => LocationService(),
  );

  // Repositories
  getIt.registerLazySingleton<AuthRepository>(
    () => AuthRepositoryImpl(apiClient: getIt<ApiClient>()),
  );

  getIt.registerLazySingleton<DriverHomeRepository>(
    () => DriverHomeRepositoryImpl(
      apiClient: getIt<ApiClient>(),
      webSocketClient: getIt<WebSocketClient>(),
    ),
  );

  getIt.registerLazySingleton<RideRepository>(
    () => RideRepositoryImpl(
      apiClient: getIt<ApiClient>(),
      webSocketClient: getIt<WebSocketClient>(),
    ),
  );

  getIt.registerLazySingleton<EarningsRepository>(
    () => EarningsRepositoryImpl(apiClient: getIt<ApiClient>()),
  );

  getIt.registerLazySingleton<DriverProfileRepository>(
    () => DriverProfileRepositoryImpl(apiClient: getIt<ApiClient>()),
  );

  // Use Cases
  getIt.registerFactory(() => LoginUseCase(getIt<AuthRepository>()));
  getIt.registerFactory(() => RegisterUseCase(getIt<AuthRepository>()));
  
  getIt.registerFactory(() => GetDriverStatsUseCase(getIt<DriverHomeRepository>()));
  getIt.registerFactory(() => ToggleAvailabilityUseCase(getIt<DriverHomeRepository>()));
  
  getIt.registerFactory(() => AcceptRideUseCase(getIt<RideRepository>()));
  getIt.registerFactory(() => StartRideUseCase(getIt<RideRepository>()));
  getIt.registerFactory(() => CompleteRideUseCase(getIt<RideRepository>()));
  
  getIt.registerFactory(() => GetDailyEarningsUseCase(getIt<EarningsRepository>()));
  getIt.registerFactory(() => GetWeeklyEarningsUseCase(getIt<EarningsRepository>()));

  // BLoCs
  getIt.registerFactory(() => AuthBloc(
    loginUseCase: getIt<LoginUseCase>(),
    registerUseCase: getIt<RegisterUseCase>(),
    authRepository: getIt<AuthRepository>(),
  ));

  getIt.registerFactory(() => DriverHomeBloc(
    getDriverStatsUseCase: getIt<GetDriverStatsUseCase>(),
    toggleAvailabilityUseCase: getIt<ToggleAvailabilityUseCase>(),
    locationService: getIt<LocationService>(),
  ));

  getIt.registerFactory(() => ActiveRideBloc(
    acceptRideUseCase: getIt<AcceptRideUseCase>(),
    startRideUseCase: getIt<StartRideUseCase>(),
    completeRideUseCase: getIt<CompleteRideUseCase>(),
    locationService: getIt<LocationService>(),
  ));

  getIt.registerFactory(() => EarningsBloc(
    getDailyEarningsUseCase: getIt<GetDailyEarningsUseCase>(),
    getWeeklyEarningsUseCase: getIt<GetWeeklyEarningsUseCase>(),
  ));

  getIt.registerFactory(() => DriverProfileBloc(
    profileRepository: getIt<DriverProfileRepository>(),
  ));

  // Router
  getIt.registerLazySingleton(() => AppRouter(authBloc: getIt<AuthBloc>()));
}