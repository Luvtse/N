import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:go_router/go_router.dart';
import 'package:get_it/get_it.dart';

import '../../features/auth/presentation/bloc/auth_bloc.dart';
import '../../features/auth/presentation/pages/login_page.dart';
import '../../features/auth/presentation/pages/register_page.dart';
import '../../features/auth/presentation/pages/onboarding_consent_page.dart';
import '../../features/auth/presentation/pages/legal_consent_page.dart';
import '../../features/home/presentation/pages/home_page.dart';
import '../../features/nidus/presentation/bloc/ride_bloc.dart';
import '../../features/nidus/presentation/pages/ride_request_page.dart';
import '../../features/nidus/presentation/pages/ride_tracking_page.dart';
import '../../features/nidus/presentation/pages/ride_history_page.dart';
import '../../features/haven/presentation/pages/hotel_search_page.dart';
import '../../features/haven/presentation/pages/hotel_detail_page.dart';
import '../../features/haven/presentation/pages/booking_page.dart';
import '../../features/vorax/presentation/pages/restaurant_list_page.dart';
import '../../features/vorax/presentation/pages/menu_page.dart';
import '../../features/vorax/presentation/pages/order_tracking_page.dart';
import '../../features/profile/presentation/pages/profile_page.dart';
import '../../features/profile/presentation/pages/settings_page.dart';
import '../../features/profile/presentation/pages/consent_management_page.dart';
import '../../features/wallet/presentation/bloc/wallet_bloc.dart';
import '../../features/wallet/presentation/pages/wallet_page.dart';

// ============================================================================
// ROUTE NAMES (for type-safe navigation)
// ============================================================================

class RouteNames {
  static const String home = 'home';
  static const String login = 'login';
  static const String register = 'register';
  static const String onboarding = 'onboarding';
  static const String rideRequest = 'rideRequest';
  static const String rideTracking = 'rideTracking';
  static const String rideHistory = 'rideHistory';
  static const String hotelSearch = 'hotelSearch';
  static const String hotelDetail = 'hotelDetail';
  static const String booking = 'booking';
  static const String restaurantList = 'restaurantList';
  static const String menu = 'menu';
  static const String orderTracking = 'orderTracking';
  static const String profile = 'profile';
  static const String settings = 'settings';
  static const String consentManagement = 'consentManagement';
  static const String legalDocument = 'legalDocument';
  static const String wallet = 'wallet';
}

// ============================================================================
// APP ROUTER
// ============================================================================

class AppRouter {
  final AuthBloc _authBloc;
  final RideBloc Function() _rideBlocFactory;
  final WalletBloc Function() _walletBlocFactory;

  late final GoRouter _router;

  AppRouter({
    required AuthBloc authBloc,
    required RideBloc Function() rideBlocFactory,
    required WalletBloc Function() walletBlocFactory,
  })  : _authBloc = authBloc,
        _rideBlocFactory = rideBlocFactory,
        _walletBlocFactory = walletBlocFactory {
    _router = GoRouter(
      initialLocation: '/splash',
      debugLogDiagnostics: true,
      refreshListenable: _authBloc, // Re-evaluate routes on auth state change
      redirect: _globalRedirect,
      routes: _buildRoutes(),
      errorBuilder: (context, state) => _ErrorPage(error: state.error),
    );
  }

  GoRouter get router => _router;

  // ============================================================================
  // GLOBAL REDIRECT (Auth Guard)
  // ============================================================================

  String? _globalRedirect(BuildContext context, GoRouterState state) {
    final authState = _authBloc.state;
    final isAuthenticated = authState is AuthAuthenticated;
    final isOnboardingComplete = authState is AuthAuthenticated && 
                                  authState.onboardingComplete;

    final isAuthRoute = state.matchedLocation == '/login' ||
                       state.matchedLocation == '/register' ||
                       state.matchedLocation == '/splash';

    // Public routes that don't require auth
    final publicRoutes = ['/login', '/register', '/splash', '/legal/'];
    final isPublicRoute = publicRoutes.any((route) => 
        state.matchedLocation.startsWith(route));

    // If not authenticated and trying to access protected route
    if (!isAuthenticated && !isPublicRoute) {
      return '/login';
    }

    // If authenticated but onboarding not complete
    if (isAuthenticated && !isOnboardingComplete && 
        state.matchedLocation != '/onboarding') {
      return '/onboarding';
    }

    // If authenticated and trying to access auth routes
    if (isAuthenticated && isAuthRoute && isOnboardingComplete) {
      return '/home';
    }

    return null; // No redirect needed
  }

  // ============================================================================
  // ROUTE TREE
  // ============================================================================

  List<GoRoute> _buildRoutes() {
    return [
      // Splash screen (initial loading)
      GoRoute(
        path: '/splash',
        name: 'splash',
        builder: (context, state) => const _SplashPage(),
      ),

      // ====================================================================
      // AUTH ROUTES (unauthenticated)
      // ====================================================================
      GoRoute(
        path: '/login',
        name: RouteNames.login,
        builder: (context, state) => const LoginPage(),
      ),
      GoRoute(
        path: '/register',
        name: RouteNames.register,
        builder: (context, state) => const RegisterPage(),
      ),
      GoRoute(
        path: '/onboarding',
        name: RouteNames.onboarding,
        builder: (context, state) => const OnboardingConsentPage(),
      ),

      // ====================================================================
      // LEGAL DOCUMENT ROUTES (public)
      // ====================================================================
      GoRoute(
        path: '/legal/:documentType',
        name: RouteNames.legalDocument,
        builder: (context, state) {
          final docType = state.pathParameters['documentType']!;
          return LegalConsentPage(
            documentType: docType,
            documentTitle: _getDocumentTitle(docType),
          );
        },
      ),

      // ====================================================================
      // SHELL ROUTE (authenticated area with bottom nav)
      // ====================================================================
      ShellRoute(
        builder: (context, state, child) => _AppShell(child: child),
        routes: [
          // Home
          GoRoute(
            path: '/home',
            name: RouteNames.home,
            builder: (context, state) => const HomePage(),
          ),

          // ==================================================================
          // NIDUS (Rides)
          // ==================================================================
          GoRoute(
            path: '/nidus/request',
            name: RouteNames.rideRequest,
            builder: (context, state) => BlocProvider(
              create: (_) => _rideBlocFactory(),
              child: const RideRequestPage(),
            ),
          ),
          GoRoute(
            path: '/nidus/tracking/:rideId',
            name: RouteNames.rideTracking,
            builder: (context, state) {
              final rideId = state.pathParameters['rideId']!;
              return BlocProvider(
                create: (_) => _rideBlocFactory(),
                child: RideTrackingPage(rideId: rideId),
              );
            },
          ),
          GoRoute(
            path: '/nidus/history',
            name: RouteNames.rideHistory,
            builder: (context, state) => const RideHistoryPage(),
          ),

          // ==================================================================
          // HAVEN (Hotels)
          // ==================================================================
          GoRoute(
            path: '/haven/search',
            name: RouteNames.hotelSearch,
            builder: (context, state) => const HotelSearchPage(),
          ),
          GoRoute(
            path: '/haven/hotel/:hotelId',
            name: RouteNames.hotelDetail,
            builder: (context, state) {
              final hotelId = state.pathParameters['hotelId']!;
              return HotelDetailPage(hotelId: hotelId);
            },
          ),
          GoRoute(
            path: '/haven/booking/:hotelId',
            name: RouteNames.booking,
            builder: (context, state) {
              final hotelId = state.pathParameters['hotelId']!;
              return BookingPage(hotelId: hotelId);
            },
          ),

          // ==================================================================
          // VORAX (Food)
          // ==================================================================
          GoRoute(
            path: '/vorax/restaurants',
            name: RouteNames.restaurantList,
            builder: (context, state) => const RestaurantListPage(),
          ),
          GoRoute(
            path: '/vorax/restaurant/:restaurantId/menu',
            name: RouteNames.menu,
            builder: (context, state) {
              final restaurantId = state.pathParameters['restaurantId']!;
              return MenuPage(restaurantId: restaurantId);
            },
          ),
          GoRoute(
            path: '/vorax/order/:orderId/tracking',
            name: RouteNames.orderTracking,
            builder: (context, state) {
              final orderId = state.pathParameters['orderId']!;
              return OrderTrackingPage(orderId: orderId);
            },
          ),

          // ==================================================================
          // PROFILE & SETTINGS
          // ==================================================================
          GoRoute(
            path: '/profile',
            name: RouteNames.profile,
            builder: (context, state) => const ProfilePage(),
          ),
          GoRoute(
            path: '/settings',
            name: RouteNames.settings,
            builder: (context, state) => const SettingsPage(),
          ),
          GoRoute(
            path: '/settings/consent',
            name: RouteNames.consentManagement,
            builder: (context, state) => const ConsentManagementPage(),
          ),

          // ==================================================================
          // WALLET (Ledger — Phase H Step 1)
          // ==================================================================
          GoRoute(
            path: '/wallet',
            name: RouteNames.wallet,
            builder: (context, state) {
              final disputeRideId = state.uri.queryParameters['disputeRideId'];
              return BlocProvider(
                create: (_) => _walletBlocFactory()..add(const LoadWallet()),
                child: WalletPage(disputeRideId: disputeRideId),
              );
            },
          ),
        ],
      ),
    ];
  }

  String _getDocumentTitle(String documentType) {
    switch (documentType) {
      case 'terms_of_service':
        return 'Terms of Service';
      case 'privacy_policy':
        return 'Privacy Policy';
      case 'cookie_policy':
        return 'Cookie Policy';
      case 'driver_terms':
        return 'Driver Terms';
      case 'payment_terms':
        return 'Payment Terms';
      case 'token_terms':
        return 'Token Terms';
      default:
        return 'Legal Document';
    }
  }
}

// ============================================================================
// APP SHELL (Bottom Navigation Bar)
// ============================================================================

class _AppShell extends StatelessWidget {
  final Widget child;

  const _AppShell({required this.child});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: child,
      bottomNavigationBar: _buildBottomNav(context),
    );
  }

  Widget _buildBottomNav(BuildContext context) {
    final currentLocation = GoRouterState.of(context).matchedLocation;
    
    return BottomNavigationBar(
      type: BottomNavigationBarType.fixed,
      currentIndex: _calculateSelectedIndex(currentLocation),
      onTap: (index) => _onItemTapped(context, index),
      items: const [
        BottomNavigationBarItem(
          icon: Icon(Icons.home_outlined),
          activeIcon: Icon(Icons.home),
          label: 'Home',
        ),
        BottomNavigationBarItem(
          icon: Icon(Icons.directions_car_outlined),
          activeIcon: Icon(Icons.directions_car),
          label: 'Rides',
        ),
        BottomNavigationBarItem(
          icon: Icon(Icons.restaurant_outlined),
          activeIcon: Icon(Icons.restaurant),
          label: 'Food',
        ),
        BottomNavigationBarItem(
          icon: Icon(Icons.hotel_outlined),
          activeIcon: Icon(Icons.hotel),
          label: 'Stays',
        ),
        BottomNavigationBarItem(
          icon: Icon(Icons.account_balance_wallet_outlined),
          activeIcon: Icon(Icons.account_balance_wallet),
          label: 'Wallet',
        ),
        BottomNavigationBarItem(
          icon: Icon(Icons.person_outline),
          activeIcon: Icon(Icons.person),
          label: 'Profile',
        ),
      ],
    );
  }

  int _calculateSelectedIndex(String location) {
    if (location.startsWith('/home')) return 0;
    if (location.startsWith('/nidus')) return 1;
    if (location.startsWith('/vorax')) return 2;
    if (location.startsWith('/haven')) return 3;
    if (location.startsWith('/wallet')) return 4;
    if (location.startsWith('/profile') || location.startsWith('/settings')) return 5;
    return 0;
  }

  void _onItemTapped(BuildContext context, int index) {
    switch (index) {
      case 0:
        context.goNamed(RouteNames.home);
        break;
      case 1:
        context.goNamed(RouteNames.rideRequest);
        break;
      case 2:
        context.goNamed(RouteNames.restaurantList);
        break;
      case 3:
        context.goNamed(RouteNames.hotelSearch);
        break;
      case 4:
        context.goNamed(RouteNames.wallet);
        break;
      case 5:
        context.goNamed(RouteNames.profile);
        break;
    }
  }
}

// ============================================================================
// SPLASH PAGE
// ============================================================================

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
    // Initialize offline service
    await GetIt.instance<OfflineService>().initialize();
    
    // Wait for auth state to be determined
    await Future.delayed(const Duration(seconds: 2));
    
    if (mounted) {
      // AuthBloc will trigger redirect based on state
      GetIt.instance<AuthBloc>().add(CheckAuthStatus());
    }
  }

  @override
  Widget build(BuildContext context) {
    return const Scaffold(
      body: Center(
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            FlutterLogo(size: 100),
            SizedBox(height: 24),
            Text(
              'NIDAW',
              style: TextStyle(
                fontSize: 32,
                fontWeight: FontWeight.bold,
              ),
            ),
            SizedBox(height: 8),
            Text('Building the Future of Urban Life'),
            SizedBox(height: 48),
            CircularProgressIndicator(),
          ],
        ),
      ),
    );
  }
}

// ============================================================================
// ERROR PAGE
// ============================================================================

class _ErrorPage extends StatelessWidget {
  final Exception? error;

  const _ErrorPage({this.error});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Page Not Found')),
      body: Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              const Icon(Icons.error_outline, size: 64, color: Colors.red),
              const SizedBox(height: 16),
              const Text(
                'Oops! Page not found',
                style: TextStyle(fontSize: 20, fontWeight: FontWeight.bold),
              ),
              const SizedBox(height: 8),
              Text(
                error?.toString() ?? 'The page you\'re looking for doesn\'t exist.',
                textAlign: TextAlign.center,
              ),
              const SizedBox(height: 24),
              ElevatedButton(
                onPressed: () => context.goNamed(RouteNames.home),
                child: const Text('Go Home'),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

// ============================================================================
// EXTENSION METHODS FOR CONVENIENCE
// ============================================================================

extension NavigationHelper on BuildContext {
  /// Navigate to home
  void goHome() => GoRouter.of(this).goNamed(RouteNames.home);
  
  /// Navigate to ride request
  void goRideRequest() => GoRouter.of(this).goNamed(RouteNames.rideRequest);
  
  /// Navigate to ride tracking
  void goRideTracking(String rideId) => 
      GoRouter.of(this).go('/nidus/tracking/$rideId');
  
  /// Navigate to hotel search
  void goHotelSearch() => GoRouter.of(this).goNamed(RouteNames.hotelSearch);
  
  /// Navigate to restaurant list
  void goRestaurants() => GoRouter.of(this).goNamed(RouteNames.restaurantList);
  
  /// Navigate to profile
  void goProfile() => GoRouter.of(this).goNamed(RouteNames.profile);

  /// Navigate to the ledger wallet
  void goWallet() => GoRouter.of(this).goNamed(RouteNames.wallet);

  /// Open the wallet with the dispute form pre-filled for a completed ride.
  void goWalletDispute(String rideId) =>
      GoRouter.of(this).go('/wallet?disputeRideId=$rideId');
  
  /// Navigate to login
  void goLogin() => GoRouter.of(this).goNamed(RouteNames.login);
  
  /// Logout and redirect to login
  void logout() {
    GetIt.instance<AuthBloc>().add(LogoutRequested());
    GoRouter.of(this).goNamed(RouteNames.login);
  }
}