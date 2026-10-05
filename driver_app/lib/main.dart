import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:firebase_core/firebase_core.dart';
import 'package:hive_flutter/hive_flutter.dart';

import 'core/di/injection.dart';
import 'core/router/app_router.dart';
import 'core/theme/app_theme.dart';
import 'features/auth/presentation/bloc/auth_bloc.dart';
import 'features/home/presentation/bloc/driver_home_bloc.dart';
import 'features/ride/presentation/bloc/active_ride_bloc.dart';
import 'features/earnings/presentation/bloc/earnings_bloc.dart';
import 'features/profile/presentation/bloc/driver_profile_bloc.dart';

void main() async {
  WidgetsFlutterBinding.ensureInitialized();

  // Initialize Firebase
  await Firebase.initializeApp();

  // Initialize Hive
  await Hive.initFlutter();
  await Hive.openBox('driver_auth');
  await Hive.openBox('driver_cache');
  await Hive.openBox('driver_settings');

  // Configure dependencies
  await configureDependencies();

  runApp(const NidawDriverApp());
}

class NidawDriverApp extends StatelessWidget {
  const NidawDriverApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MultiBlocProvider(
      providers: [
        BlocProvider(
          create: (_) => getIt<AuthBloc>()..add(const CheckAuthStatus()),
        ),
        BlocProvider(
          create: (_) => getIt<DriverHomeBloc>(),
        ),
        BlocProvider(
          create: (_) => getIt<ActiveRideBloc>(),
        ),
        BlocProvider(
          create: (_) => getIt<EarningsBloc>(),
        ),
        BlocProvider(
          create: (_) => getIt<DriverProfileBloc>(),
        ),
      ],
      child: MaterialApp.router(
        title: 'NIDAW Driver',
        debugShowCheckedModeBanner: false,
        theme: AppTheme.light,
        darkTheme: AppTheme.dark,
        themeMode: ThemeMode.system,
        routerConfig: getIt<AppRouter>().router,
      ),
    );
  }
}