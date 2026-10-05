import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

import '../../features/auth/presentation/bloc/auth_bloc.dart';
import '../../features/auth/presentation/pages/login_page.dart';
import '../../features/auth/presentation/pages/register_page.dart';
import '../../features/auth/presentation/pages/onboarding_page.dart';

import '../../features/home/presentation/pages/driver_home_page.dart';

import '../../features/ride/presentation/pages/ride_acceptance_page.dart';
import '../../features/ride/presentation/pages/ride_navigation_page.dart';
import '../../features/ride/presentation/pages/ride_completion_page.dart';

import '../../features/earnings/presentation/pages/earnings_overview_page.dart';
import '../../features/earnings/presentation/pages/earnings_detail_page.dart';

import '../../features/documents/presentation/pages/documents_page.dart';

import '../../features/profile/presentation/pages/driver_profile_page.dart';
import '../../features/profile/presentation/pages/vehicle_info_page.dart';
import '../../features/profile/presentation/pages/settings_page.dart';

class AppRouter {
  final AuthBloc _authBloc;
  late final GoRouter _router;

  AppRouter({required AuthBloc authBloc}) : _authBloc = authBloc {
    _router = GoRouter(
      initialLocation: '/splash',
      refreshListenable: _authBloc,
      redirect: _globalRedirect,
      routes: [
        GoRoute(
          path: '/splash',
          builder: (context, state) => const _SplashPage(),
        ),
        
        // Auth routes
        GoRoute(
          path: '/login',
          builder: (context, state) => const LoginPage(),
        ),
        GoRoute(
          path: '/register',
          builder: (context, state) => const RegisterPage(),
        ),
        GoRoute(
          path: '/onboarding',
          builder: (context, state) => const OnboardingPage(),
        ),

        // Home
        GoRoute(
          path: '/home',
          builder: (context, state) => const DriverHomePage(),
        ),

        // Ride routes
        GoRoute(
          path: '/ride/accept/:rideId',
          builder: (context, state) => RideAcceptancePage(
            rideId: state.pathParameters['rideId']!,
          ),
        ),
        GoRoute(
          path: '/ride/navigate/:rideId',
          builder: (context, state) => RideNavigationPage(
            rideId: state.pathParameters['rideId']!,
          ),
        ),
        GoRoute(
          path: '/ride/complete/:rideId',
          builder: (context, state) => RideCompletionPage(
            rideId: state.pathParameters['rideId']!,
          ),
        ),

        // Earnings
        GoRoute(
          path: '/earnings',
          builder: (context, state) => const EarningsOverviewPage(),
        ),
        GoRoute(
          path: '/earnings/detail',
          builder: (context, state) => const EarningsDetailPage(),
        ),

        // Documents
        GoRoute(
          path: '/documents',
          builder: (context, state) => const DocumentsPage(),
        ),

        // Profile
        GoRoute(
          path: '/profile',
          builder: (context, state) => const DriverProfilePage(),
        ),
        GoRoute(
          path: '/profile/vehicle',
          builder: (context, state) => const VehicleInfoPage(),
        ),
        GoRoute(
          path: '/settings',
          builder: (context, state) => const SettingsPage(),
        ),
      ],
    );
  }

  String? _globalRedirect(BuildContext context, GoRouterState state) {
    final authState = _authBloc.state;
    final isAuthenticated = authState is AuthAuthenticated;
    final isOnboardingComplete = authState is AuthAuthenticated && 
                                  authState.onboardingComplete;

    final isAuthRoute = state.matchedLocation == '/login' ||
                       state.matchedLocation == '/register' ||
                       state.matchedLocation == '/splash';

    if (!isAuthenticated && !isAuthRoute) {
      return '/login';
    }

    if (isAuthenticated && !isOnboardingComplete && 
        state.matchedLocation != '/onboarding') {
      return '/onboarding';
    }

    if (isAuthenticated && isAuthRoute && isOnboardingComplete) {
      return '/home';
    }

    return null;
  }

  GoRouter get router => _router;
}

class _SplashPage extends StatefulWidget {
  const _SplashPage();

  @override
  State<_SplashPage> createState() => _SplashPageState();
}

class _SplashPageState extends State<_SplashPage> {
  @override
  void initState() {
    super.initState();
    _initialize();
  }

  Future<void> _initialize() async {
    await Future.delayed(const Duration(seconds: 2));
    if (mounted) {
      _authBloc.add(const CheckAuthStatus());
    }
  }

  @override
  Widget build(BuildContext context) {
    return const Scaffold(
      body: Center(
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Icon(Icons.directions_car, size: 100, color: Colors.green),
            SizedBox(height: 24),
            Text(
              'NIDAW Driver',
              style: TextStyle(
                fontSize: 32,
                fontWeight: FontWeight.bold,
              ),
            ),
            SizedBox(height: 48),
            CircularProgressIndicator(),
          ],
        ),
      ),
    );
  }
}