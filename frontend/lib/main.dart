import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:firebase_core/firebase_core.dart';

import 'core/di/injection.dart';
import 'core/notifications/fcm_service.dart';
import 'core/router/app_router.dart';
import 'core/theme/app_theme.dart';
import 'core/network/websocket_client.dart';
import 'core/offline/offline_service.dart';
import 'features/auth/presentation/bloc/auth_bloc.dart';
import 'features/auth/presentation/bloc/legal_consent_bloc.dart';
import 'features/nidus/presentation/bloc/ride_bloc.dart';
import 'features/profile/presentation/bloc/profile_bloc.dart';
import 'l10n/l10n.dart';

void main() async {
  WidgetsFlutterBinding.ensureInitialized();

  // Initialize Firebase
  await Firebase.initializeApp();

  // Configure dependencies
  await configureDependencies();

  // Initialize services
  await getIt<OfflineService>().initialize();
  await getIt<WebSocketClient>().connect();

  // Push substrate (§1.4). initialize() requests permission (iOS + Android
  // 13+), fetches the FCM token, registers it with the backend, and wires
  // foreground/background/killed handlers. It returns false when permission
  // is denied — in-app WS banner + inbox still work, so we do not block
  // startup on it; failures are surfaced honestly via those channels.
  await getIt<FcmService>().initialize();

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
      // §4.3 Localization: the settings language switcher writes through
      // LocaleController.saveCode(), which pushes onto this notifier. The
      // root rebuilds with the new locale immediately; null = system default.
      child: ValueListenableBuilder<Locale?>(
        valueListenable: LocaleController.apply,
        builder: (context, savedLocale, _) {
          return MaterialApp.router(
            title: 'NIDAW',
            debugShowCheckedModeBanner: false,
            theme: AppTheme.light,
            darkTheme: AppTheme.dark,
            themeMode: ThemeMode.system,
            routerConfig: getIt<AppRouter>().router,
            locale: savedLocale,
            supportedLocales: LocaleController.supportedLocales,
            localizationsDelegates: const [
              AppLocalizations.delegate,
              GlobalMaterialLocalizations.delegate,
              GlobalWidgetsLocalizations.delegate,
              GlobalCupertinoLocalizations.delegate,
            ],
          );
        },
      ),
    );
  }
}