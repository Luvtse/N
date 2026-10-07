import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:go_router/go_router.dart';

import '../../../../core/theme/app_colors.dart';
import '../../../../core/router/app_router.dart';
import '../../../auth/presentation/bloc/auth_bloc.dart';
import '../widgets/service_card.dart';
import '../widgets/promo_banner.dart';
import '../widgets/quick_stats.dart';

class HomePage extends StatelessWidget {
  const HomePage({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: AppColors.background,
      body: BlocBuilder<AuthBloc, AuthState>(
        builder: (context, state) {
          final user = state is AuthAuthenticated ? state.user : null;

          return CustomScrollView(
            slivers: [
              // ====================================================================
              // HERO SECTION WITH GRADIENT
              // ====================================================================
              SliverToBoxAdapter(
                child: _buildHeroSection(context, user),
              ),

              // ====================================================================
              // SERVICES GRID
              // ====================================================================
              SliverPadding(
                padding: const EdgeInsets.all(20),
                sliver: SliverToBoxAdapter(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      _buildSectionTitle('Our Services'),
                      const SizedBox(height: 16),
                      _buildServicesGrid(context),
                    ],
                  ),
                ),
              ),

              // ====================================================================
              // PROMOTIONAL BANNER
              // ====================================================================
              SliverPadding(
                padding: const EdgeInsets.symmetric(horizontal: 20),
                sliver: SliverToBoxAdapter(
                  child: PromoBanner(
                    title: 'Earn NIDAW Tokens',
                    subtitle: 'Get 50 NIDAW tokens on your first ride',
                    icon: Icons.card_giftcard,
                    gradient: const LinearGradient(
                      colors: [Color(0xFFF59E0B), Color(0xFFEF4444)],
                      begin: Alignment.topLeft,
                      end: Alignment.bottomRight,
                    ),
                    onTap: () {
                      // Navigate to token rewards
                    },
                  ),
                ),
              ),

              const SliverToBoxAdapter(child: SizedBox(height: 20)),

              // ====================================================================
              // QUICK STATS
              // ====================================================================
              SliverPadding(
                padding: const EdgeInsets.symmetric(horizontal: 20),
                sliver: SliverToBoxAdapter(
                  child: QuickStats(
                    stats: const [
                      QuickStat(
                        icon: Icons.directions_car,
                        label: 'Rides',
                        value: '12',
                        color: AppColors.nidusBlue,
                      ),
                      QuickStat(
                        icon: Icons.hotel,
                        label: 'Stays',
                        value: '3',
                        color: AppColors.havenPurple,
                      ),
                      QuickStat(
                        icon: Icons.restaurant,
                        label: 'Orders',
                        value: '28',
                        color: AppColors.voraxOrange,
                      ),
                    ],
                  ),
                ),
              ),

              const SliverToBoxAdapter(child: SizedBox(height: 20)),

              // ====================================================================
              // RECENT ACTIVITY
              // ====================================================================
              SliverPadding(
                padding: const EdgeInsets.all(20),
                sliver: SliverToBoxAdapter(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      _buildSectionTitle('Recent Activity'),
                      const SizedBox(height: 16),
                      _buildRecentActivity(),
                    ],
                  ),
                ),
              ),

              const SliverToBoxAdapter(child: SizedBox(height: 100)),
            ],
          );
        },
      ),
      bottomNavigationBar: _buildBottomNav(context),
    );
  }

  // ==========================================================================
  // HERO SECTION
  // ==========================================================================

  Widget _buildHeroSection(BuildContext context, dynamic user) {
    return Container(
      decoration: const BoxDecoration(
        gradient: LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [
            Color(0xFF1E40AF), // Primary Blue
            Color(0xFF0D9488), // Secondary Teal
          ],
        ),
      ),
      child: SafeArea(
        bottom: false,
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              // Top Row: Greeting + Actions
              Row(
                children: [
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          'Good ${_getTimeOfDay()},',
                          style: const TextStyle(
                            color: Colors.white70,
                            fontSize: 16,
                          ),
                        ),
                        const SizedBox(height: 4),
                        Text(
                          user?.fullName ?? 'Traveler',
                          style: const TextStyle(
                            color: Colors.white,
                            fontSize: 28,
                            fontWeight: FontWeight.bold,
                          ),
                        ),
                      ],
                    ),
                  ),
                  _buildIconButton(
                    icon: Icons.notifications_outlined,
                    onTap: () {},
                  ),
                  const SizedBox(width: 12),
                  _buildIconButton(
                    icon: Icons.person_outline,
                    onTap: () => context.push('/profile'),
                  ),
                ],
              ),

              const SizedBox(height: 24),

              // Wallet Card (taps through to the ledger wallet — Phase H)
              InkWell(
                onTap: () => context.goNamed(RouteNames.wallet),
                borderRadius: BorderRadius.circular(16),
                child: Container(
                  padding: const EdgeInsets.all(20),
                  decoration: BoxDecoration(
                    color: Colors.white.withOpacity(0.15),
                    borderRadius: BorderRadius.circular(16),
                    border: Border.all(
                      color: Colors.white.withOpacity(0.2),
                    ),
                  ),
                  child: Row(
                    children: [
                      Container(
                        padding: const EdgeInsets.all(12),
                        decoration: BoxDecoration(
                          color: Colors.white.withOpacity(0.2),
                          borderRadius: BorderRadius.circular(12),
                        ),
                        child: const Icon(
                          Icons.account_balance_wallet,
                          color: Colors.white,
                          size: 28,
                        ),
                      ),
                      const SizedBox(width: 16),
                      const Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(
                              'Wallet Balance',
                              style: TextStyle(
                                color: Colors.white70,
                                fontSize: 14,
                              ),
                            ),
                            SizedBox(height: 4),
                            Text(
                              '\$245.50',
                              style: TextStyle(
                                color: Colors.white,
                                fontSize: 24,
                                fontWeight: FontWeight.bold,
                              ),
                            ),
                          ],
                        ),
                      ),
                      ElevatedButton(
                        onPressed: () => context.goNamed(RouteNames.wallet),
                        style: ElevatedButton.styleFrom(
                          backgroundColor: Colors.white,
                          foregroundColor: AppColors.primary,
                          padding: const EdgeInsets.symmetric(
                            horizontal: 20,
                            vertical: 12,
                          ),
                          shape: RoundedRectangleBorder(
                            borderRadius: BorderRadius.circular(12),
                          ),
                        ),
                        child: const Text(
                          'Top Up',
                          style: TextStyle(fontWeight: FontWeight.w600),
                        ),
                      ),
                    ],
                  ),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildIconButton({
    required IconData icon,
    required VoidCallback onTap,
  }) {
    return GestureDetector(
      onTap: onTap,
      child: Container(
        padding: const EdgeInsets.all(10),
        decoration: BoxDecoration(
          color: Colors.white.withOpacity(0.15),
          shape: BoxShape.circle,
        ),
        child: Icon(icon, color: Colors.white, size: 24),
      ),
    );
  }

  String _getTimeOfDay() {
    final hour = DateTime.now().hour;
    if (hour < 12) return 'morning';
    if (hour < 17) return 'afternoon';
    return 'evening';
  }

  // ==========================================================================
  // SECTION TITLE
  // ==========================================================================

  Widget _buildSectionTitle(String title) {
    return Text(
      title,
      style: const TextStyle(
        fontSize: 20,
        fontWeight: FontWeight.bold,
        color: AppColors.textPrimary,
      ),
    );
  }

  // ==========================================================================
  // SERVICES GRID
  // ==========================================================================

  Widget _buildServicesGrid(BuildContext context) {
    return GridView.count(
      shrinkWrap: true,
      physics: const NeverScrollableScrollPhysics(),
      crossAxisCount: 2,
      mainAxisSpacing: 16,
      crossAxisSpacing: 16,
      childAspectRatio: 1.1,
      children: [
        ServiceCard(
          title: 'Rides',
          subtitle: 'Get a ride now',
          icon: Icons.directions_car_filled,
          color: AppColors.nidusBlue,
          onTap: () => context.push('/nidus/request'),
        ),
        ServiceCard(
          title: 'Hotels',
          subtitle: 'Book your stay',
          icon: Icons.hotel,
          color: AppColors.havenPurple,
          onTap: () => context.push('/haven/search'),
        ),
        ServiceCard(
          title: 'Food',
          subtitle: 'Order delivery',
          icon: Icons.restaurant,
          color: AppColors.voraxOrange,
          onTap: () => context.push('/vorax/restaurants'),
        ),
        ServiceCard(
          title: 'Freight',
          subtitle: 'Ship cargo',
          icon: Icons.local_shipping,
          color: AppColors.logixGreen,
          onTap: () {
            // TODO: Navigate to freight
          },
        ),
      ],
    );
  }

  // ==========================================================================
  // RECENT ACTIVITY
  // ==========================================================================

  Widget _buildRecentActivity() {
    return Column(
      children: [
        _buildActivityItem(
          icon: Icons.directions_car,
          iconColor: AppColors.nidusBlue,
          title: 'Ride to Downtown',
          subtitle: 'Yesterday, 2:30 PM',
          amount: '\$15.50',
        ),
        const SizedBox(height: 12),
        _buildActivityItem(
          icon: Icons.restaurant,
          iconColor: AppColors.voraxOrange,
          title: 'Pizza Order',
          subtitle: '2 days ago',
          amount: '\$24.99',
        ),
        const SizedBox(height: 12),
        _buildActivityItem(
          icon: Icons.hotel,
          iconColor: AppColors.havenPurple,
          title: 'Hotel Booking',
          subtitle: 'Last week',
          amount: '\$189.00',
        ),
      ],
    );
  }

  Widget _buildActivityItem({
    required IconData icon,
    required Color iconColor,
    required String title,
    required String subtitle,
    required String amount,
  }) {
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.circular(16),
        boxShadow: [
          BoxShadow(
            color: Colors.black.withOpacity(0.04),
            blurRadius: 10,
            offset: const Offset(0, 2),
          ),
        ],
      ),
      child: Row(
        children: [
          Container(
            width: 48,
            height: 48,
            decoration: BoxDecoration(
              color: iconColor.withOpacity(0.1),
              borderRadius: BorderRadius.circular(12),
            ),
            child: Icon(icon, color: iconColor, size: 24),
          ),
          const SizedBox(width: 16),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  title,
                  style: const TextStyle(
                    fontSize: 16,
                    fontWeight: FontWeight.w600,
                    color: AppColors.textPrimary,
                  ),
                ),
                const SizedBox(height: 4),
                Text(
                  subtitle,
                  style: TextStyle(
                    fontSize: 13,
                    color: Colors.grey[600],
                  ),
                ),
              ],
            ),
          ),
          Text(
            amount,
            style: TextStyle(
              fontSize: 16,
              fontWeight: FontWeight.bold,
              color: iconColor,
            ),
          ),
        ],
      ),
    );
  }

  // ==========================================================================
  // BOTTOM NAVIGATION
  // ==========================================================================

  Widget _buildBottomNav(BuildContext context) {
    return Container(
      decoration: BoxDecoration(
        color: Colors.white,
        boxShadow: [
          BoxShadow(
            color: Colors.black.withOpacity(0.05),
            blurRadius: 20,
            offset: const Offset(0, -2),
          ),
        ],
      ),
      child: BottomNavigationBar(
        type: BottomNavigationBarType.fixed,
        currentIndex: 0,
        selectedItemColor: AppColors.primary,
        unselectedItemColor: Colors.grey[400],
        selectedFontSize: 12,
        unselectedFontSize: 12,
        elevation: 0,
        onTap: (index) {
          switch (index) {
            case 0:
              // Home (current)
              break;
            case 1:
              context.push('/nidus/history');
              break;
            case 2:
              // Activity
              break;
            case 3:
              context.push('/profile');
              break;
          }
        },
        items: const [
          BottomNavigationBarItem(
            icon: Icon(Icons.home_outlined),
            activeIcon: Icon(Icons.home),
            label: 'Home',
          ),
          BottomNavigationBarItem(
            icon: Icon(Icons.history_outlined),
            activeIcon: Icon(Icons.history),
            label: 'History',
          ),
          BottomNavigationBarItem(
            icon: Icon(Icons.favorite_border),
            activeIcon: Icon(Icons.favorite),
            label: 'Saved',
          ),
          BottomNavigationBarItem(
            icon: Icon(Icons.person_outline),
            activeIcon: Icon(Icons.person),
            label: 'Profile',
          ),
        ],
      ),
    );
  }
}