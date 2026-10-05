import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:go_router/go_router.dart';

import '../../../../core/theme/app_colors.dart';
import '../bloc/driver_home_bloc.dart';
import '../widgets/availability_toggle.dart';
import '../widgets/earnings_summary.dart';
import '../widgets/driver_map.dart';
import '../widgets/ride_request_card.dart';

class DriverHomePage extends StatefulWidget {
  const DriverHomePage({super.key});

  @override
  State<DriverHomePage> createState() => _DriverHomePageState();
}

class _DriverHomePageState extends State<DriverHomePage> {
  @override
  void initState() {
    super.initState();
    context.read<DriverHomeBloc>().add(const LoadDriverStats());
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('NIDAW Driver'),
        actions: [
          IconButton(
            icon: const Icon(Icons.notifications_outlined),
            onPressed: () {
              // TODO: Navigate to notifications
            },
          ),
          IconButton(
            icon: const Icon(Icons.menu),
            onPressed: () => _showMenu(context),
          ),
        ],
      ),
      body: BlocConsumer<DriverHomeBloc, DriverHomeState>(
        listener: (context, state) {
          if (state is DriverHomeError) {
            ScaffoldMessenger.of(context).showSnackBar(
              SnackBar(
                content: Text(state.message),
                backgroundColor: AppColors.error,
              ),
            );
          }
        },
        builder: (context, state) {
          if (state is DriverHomeLoading) {
            return const Center(child: CircularProgressIndicator());
          }

          if (state is DriverHomeLoaded) {
            return Column(
              children: [
                // Availability Toggle
                AvailabilityToggle(
                  isOnline: state.isOnline,
                  onChanged: (isOnline) {
                    context.read<DriverHomeBloc>().add(
                          ToggleAvailability(isOnline: isOnline),
                        );
                  },
                ),

                // Map
                Expanded(
                  flex: 3,
                  child: DriverMap(
                    currentPosition: state.currentPosition,
                    isOnline: state.isOnline,
                  ),
                ),

                // Bottom Panel
                Expanded(
                  flex: 2,
                  child: Container(
                    padding: const EdgeInsets.all(20),
                    decoration: const BoxDecoration(
                      color: Colors.white,
                      borderRadius: BorderRadius.only(
                        topLeft: Radius.circular(24),
                        topRight: Radius.circular(24),
                      ),
                      boxShadow: [
                        BoxShadow(
                          color: Colors.black12,
                          blurRadius: 10,
                          offset: Offset(0, -2),
                        ),
                      ],
                    ),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        // Today's Earnings
                        EarningsSummary(
                          todayEarnings: state.stats.todayEarnings,
                          todayRides: state.stats.todayRides,
                          onlineHours: state.stats.onlineHours,
                        ),

                        const SizedBox(height: 20),

                        // Quick Stats
                        Row(
                          children: [
                            _buildQuickStat(
                              icon: Icons.star,
                              label: 'Rating',
                              value: state.stats.averageRating.toStringAsFixed(1),
                              color: Colors.amber,
                            ),
                            const SizedBox(width: 12),
                            _buildQuickStat(
                              icon: Icons.check_circle,
                              label: 'Acceptance',
                              value: '${state.stats.acceptanceRate.toInt()}%',
                              color: AppColors.success,
                            ),
                            const SizedBox(width: 12),
                            _buildQuickStat(
                              icon: Icons.speed,
                              label: 'Trips',
                              value: state.stats.totalRides.toString(),
                              color: AppColors.primary,
                            ),
                          ],
                        ),
                      ],
                    ),
                  ),
                ),
              ],
            );
          }

          return const Center(child: Text('Failed to load driver data'));
        },
      ),
    );
  }

  Widget _buildQuickStat({
    required IconData icon,
    required String label,
    required String value,
    required Color color,
  }) {
    return Expanded(
      child: Container(
        padding: const EdgeInsets.all(12),
        decoration: BoxDecoration(
          color: color.withOpacity(0.1),
          borderRadius: BorderRadius.circular(12),
        ),
        child: Column(
          children: [
            Icon(icon, color: color, size: 24),
            const SizedBox(height: 8),
            Text(
              value,
              style: TextStyle(
                fontSize: 18,
                fontWeight: FontWeight.bold,
                color: color,
              ),
            ),
            Text(
              label,
              style: TextStyle(
                fontSize: 12,
                color: Colors.grey[600],
              ),
            ),
          ],
        ),
      ),
    );
  }

  void _showMenu(BuildContext context) {
    showModalBottomSheet(
      context: context,
      builder: (context) => Container(
        padding: const EdgeInsets.all(20),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            ListTile(
              leading: const Icon(Icons.account_balance_wallet),
              title: const Text('Earnings'),
              onTap: () {
                Navigator.pop(context);
                context.push('/earnings');
              },
            ),
            ListTile(
              leading: const Icon(Icons.description),
              title: const Text('Documents'),
              onTap: () {
                Navigator.pop(context);
                context.push('/documents');
              },
            ),
            ListTile(
              leading: const Icon(Icons.person),
              title: const Text('Profile'),
              onTap: () {
                Navigator.pop(context);
                context.push('/profile');
              },
            ),
            ListTile(
              leading: const Icon(Icons.settings),
              title: const Text('Settings'),
              onTap: () {
                Navigator.pop(context);
                context.push('/settings');
              },
            ),
            ListTile(
              leading: const Icon(Icons.help),
              title: const Text('Support'),
              onTap: () {
                Navigator.pop(context);
                // TODO: Open support
              },
            ),
            ListTile(
              leading: const Icon(Icons.logout, color: AppColors.error),
              title: const Text(
                'Logout',
                style: TextStyle(color: AppColors.error),
              ),
              onTap: () {
                Navigator.pop(context);
                context.read<AuthBloc>().add(const LogoutRequested());
              },
            ),
          ],
        ),
      ),
    );
  }
}