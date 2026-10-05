import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:firebase_core/firebase_core.dart';

import 'core/di/injection.dart';
import 'core/router/app_router.dart';
import 'core/theme/app_theme.dart';
import 'core/network/websocket_client.dart';
import 'core/offline/offline_service.dart';
import 'features/auth/presentation/bloc/auth_bloc.dart';
import 'features/auth/presentation/bloc/legal_consent_bloc.dart';
import 'features/nidus/presentation/bloc/ride_bloc.dart';
import 'features/profile/presentation/bloc/profile_bloc.dart';

void main() async {
  WidgetsFlutterBinding.ensureInitialized();

  // Initialize Firebase
  await Firebase.initializeApp();

  // Configure dependencies
  await configureDependencies();

  // Initialize services
  await getIt<OfflineService>().initialize();
  await getIt<WebSocketClient>().connect();

  runApp(const NidawApp());
}

class NidawApp extends StatelessWidget {
  const NidawApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MultiBlocProvider(
      providers: [
        BlocProvider(
          create: (_) => getIt<AuthBloc>()..add(const CheckAuthStatus()),
        ),
        BlocProvider(
          create: (_) => getIt<LegalConsentBloc>(),
        ),
        BlocProvider(
          create: (_) => getIt<RideBloc>(),
        ),
        BlocProvider(
          create: (_) => getIt<ProfileBloc>(),
        ),
      ],
      child: MaterialApp.router(
        title: 'NIDAW',
        debugShowCheckedModeBanner: false,
        theme: AppTheme.light,
        darkTheme: AppTheme.dark,
        themeMode: ThemeMode.system,
        routerConfig: getIt<AppRouter>().router,
      ),
    );
  }
}