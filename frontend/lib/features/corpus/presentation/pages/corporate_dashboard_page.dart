import 'package:flutter/material.dart';
import 'package:fl_chart/fl_chart.dart';
import '../../../../core/theme/app_colors.dart';

class CorporateDashboardPage extends StatelessWidget {
  const CorporateDashboardPage({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Corporate Travel'),
        actions: [
          IconButton(
            icon: const Icon(Icons.notifications_outlined),
            onPressed: () {},
          ),
          IconButton(
            icon: const Icon(Icons.settings),
            onPressed: () {},
          ),
        ],
      ),
      body: SingleChildScrollView(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            // Company Header
            _buildCompanyHeader(),
            const SizedBox(height: 24),

            // KPI Cards
            _buildKPICards(),
            const SizedBox(height: 24),

            // Spending Chart
            _buildSpendingChart(),
            const SizedBox(height: 24),

            // Policy Compliance
            _buildPolicyCompliance(),
            const SizedBox(height: 24),

            // Pending Approvals
            _buildPendingApprovals(),
            const SizedBox(height: 24),

            // Quick Actions
            _buildQuickActions(context),
          ],
        ),
      ),
    );
  }

  Widget _buildCompanyHeader() {
    return Container(
      padding: const EdgeInsets.all(20),
      decoration: BoxDecoration(
        gradient: const LinearGradient(
          colors: [AppColors.corpusTeal, Color(0xFF0D9488)],
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
        ),
        borderRadius: BorderRadius.circular(16),
      ),
      child: Row(
        children: [
          Container(
            width: 60,
            height: 60,
            decoration: BoxDecoration(
              color: Colors.white,
              borderRadius: BorderRadius.circular(12),
            ),
            child: const Icon(Icons.business, color: AppColors.corpusTeal, size: 32),
          ),
          const SizedBox(width: 16),
          const Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  'Acme Corporation',
                  style: TextStyle(
                    color: Colors.white,
                    fontSize: 20,
                    fontWeight: FontWeight.bold,
                  ),
                ),
                SizedBox(height: 4),
                Text(
                  'Enterprise Plan • 250 employees',
                  style: TextStyle(color: Colors.white70),
                ),
              ],
            ),
          ),
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
            decoration: BoxDecoration(
              color: Colors.white.withOpacity(0.2),
              borderRadius: BorderRadius.circular(20),
            ),
            child: const Text(
              'Active',
              style: TextStyle(color: Colors.white, fontWeight: FontWeight.w600),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildKPICards() {
    return GridView.count(
      shrinkWrap: true,
      physics: const NeverScrollableScrollPhysics(),
      crossAxisCount: 2,
      mainAxisSpacing: 12,
      crossAxisSpacing: 12,
      childAspectRatio: 1.5,
      children: [
        _buildKPICard(
          title: 'Monthly Spend',
          value: '\$124,580',
          change: '+12.5%',
          changePositive: false,
          icon: Icons.account_balance_wallet,
          color: AppColors.primary,
        ),
        _buildKPICard(
          title: 'Active Bookings',
          value: '87',
          change: '+8',
          changePositive: true,
          icon: Icons.bookmark,
          color: AppColors.success,
        ),
        _buildKPICard(
          title: 'Policy Compliance',
          value: '94.2%',
          change: '+2.1%',
          changePositive: true,
          icon: Icons.verified,
          color: AppColors.secondary,
        ),
        _buildKPICard(
          title: 'Pending Approvals',
          value: '12',
          change: '-3',
          changePositive: true,
          icon: Icons.pending_actions,
          color: AppColors.warning,
        ),
      ],
    );
  }

  Widget _buildKPICard({
    required String title,
    required String value,
    required String change,
    required bool changePositive,
    required IconData icon,
    required Color color,
  }) {
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: color.withOpacity(0.2)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(icon, color: color, size: 24),
              const Spacer(),
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
                decoration: BoxDecoration(
                  color: changePositive ? AppColors.success.withOpacity(0.1) : AppColors.error.withOpacity(0.1),
                  borderRadius: BorderRadius.circular(8),
                ),
                child: Text(
                  change,
                  style: TextStyle(
                    color: changePositive ? AppColors.success : AppColors.error,
                    fontSize: 12,
                    fontWeight: FontWeight.w600,
                  ),
                ),
              ),
            ],
          ),
          const SizedBox(height: 12),
          Text(
            value,
            style: const TextStyle(fontSize: 24, fontWeight: FontWeight.bold),
          ),
          const SizedBox(height: 4),
          Text(
            title,
            style: TextStyle(fontSize: 12, color: Colors.grey[600]),
          ),
        ],
      ),
    );
  }

  Widget _buildSpendingChart() {
    return Container(
      padding: const EdgeInsets.all(20),
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.circular(16),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text(
            'Spending by Service',
            style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold),
          ),
          const SizedBox(height: 20),
          SizedBox(
            height: 200,
            child: PieChart(
              PieChartData(
                sections: [
                  PieChartSectionData(
                    value: 45,
                    color: AppColors.nidusBlue,
                    title: 'Rides\n45%',
                    radius: 80,
                  ),
                  PieChartSectionData(
                    value: 30,
                    color: AppColors.havenPurple,
                    title: 'Hotels\n30%',
                    radius: 80,
                  ),
                  PieChartSectionData(
                    value: 15,
                    color: AppColors.voraxOrange,
                    title: 'Food\n15%',
                    radius: 80,
                  ),
                  PieChartSectionData(
                    value: 10,
                    color: AppColors.logixGreen,
                    title: 'Freight\n10%',
                    radius: 80,
                  ),
                ],
                sectionsSpace: 2,
                centerSpaceRadius: 40,
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildPolicyCompliance() {
    return Container(
      padding: const EdgeInsets.all(20),
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.circular(16),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text(
            'Policy Compliance',
            style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold),
          ),
          const SizedBox(height: 16),
          _buildComplianceRow('Ride Policy', 96),
          const SizedBox(height: 12),
          _buildComplianceRow('Hotel Policy', 92),
          const SizedBox(height: 12),
          _buildComplianceRow('Food Policy', 88),
          const SizedBox(height: 12),
          _buildComplianceRow('Freight Policy', 95),
        ],
      ),
    );
  }

  Widget _buildComplianceRow(String policy, int percentage) {
    Color color;
    if (percentage >= 95) color = AppColors.success;
    else if (percentage >= 85) color = AppColors.warning;
    else color = AppColors.error;

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          mainAxisAlignment: MainAxisAlignment.spaceBetween,
          children: [
            Text(policy, style: const TextStyle(fontWeight: FontWeight.w500)),
            Text(
              '$percentage%',
              style: TextStyle(color: color, fontWeight: FontWeight.bold),
            ),
          ],
        ),
        const SizedBox(height: 8),
        ClipRRect(
          borderRadius: BorderRadius.circular(4),
          child: LinearProgressIndicator(
            value: percentage / 100,
            backgroundColor: Colors.grey[200],
            valueColor: AlwaysStoppedAnimation<Color>(color),
            minHeight: 8,
          ),
        ),
      ],
    );
  }

  Widget _buildPendingApprovals() {
    return Container(
      padding: const EdgeInsets.all(20),
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.circular(16),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            mainAxisAlignment: MainAxisAlignment.spaceBetween,
            children: [
              const Text(
                'Pending Approvals',
                style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold),
              ),
              TextButton(
                onPressed: () {},
                child: const Text('View All'),
              ),
            ],
          ),
          const SizedBox(height: 12),
          _buildApprovalItem(
            employee: 'Sarah Johnson',
            service: 'Hotel Booking',
            amount: '\$1,250',
            reason: 'Exceeds policy limit',
          ),
          _buildApprovalItem(
            employee: 'Mike Chen',
            service: 'Ride',
            amount: '\$85',
            reason: 'Weekend travel',
          ),
          _buildApprovalItem(
            employee: 'Emily Davis',
            service: 'Freight',
            amount: '\$4,500',
            reason: 'New carrier',
          ),
        ],
      ),
    );
  }

  Widget _buildApprovalItem({
    required String employee,
    required String service,
    required String amount,
    required String reason,
  }) {
    return Container(
      margin: const EdgeInsets.only(bottom: 12),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: Colors.grey[50],
        borderRadius: BorderRadius.circular(12),
      ),
      child: Row(
        children: [
          const CircleAvatar(
            child: Icon(Icons.person),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(employee, style: const TextStyle(fontWeight: FontWeight.w600)),
                Text('$service • $amount', style: TextStyle(color: Colors.grey[600], fontSize: 12)),
                Text(reason, style: TextStyle(color: AppColors.warning, fontSize: 12)),
              ],
            ),
          ),
          Column(
            children: [
              IconButton(
                icon: const Icon(Icons.check_circle, color: AppColors.success),
                onPressed: () {},
              ),
              IconButton(
                icon: const Icon(Icons.cancel, color: AppColors.error),
                onPressed: () {},
              ),
            ],
          ),
        ],
      ),
    );
  }

  Widget _buildQuickActions(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(20),
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.circular(16),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text(
            'Quick Actions',
            style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold),
          ),
          const SizedBox(height: 16),
          Row(
            children: [
              Expanded(
                child: _buildActionButton(
                  icon: Icons.policy,
                  label: 'Manage Policies',
                  onTap: () {},
                ),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: _buildActionButton(
                  icon: Icons.people,
                  label: 'Employees',
                  onTap: () {},
                ),
              ),
            ],
          ),
          const SizedBox(height: 12),
          Row(
            children: [
              Expanded(
                child: _buildActionButton(
                  icon: Icons.receipt_long,
                  label: 'Expense Reports',
                  onTap: () {},
                ),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: _buildActionButton(
                  icon: Icons.analytics,
                  label: 'Reports',
                  onTap: () {},
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }

  Widget _buildActionButton({
    required IconData icon,
    required String label,
    required VoidCallback onTap,
  }) {
    return InkWell(
      onTap: onTap,
      child: Container(
        padding: const EdgeInsets.symmetric(vertical: 20),
        decoration: BoxDecoration(
          color: AppColors.corpusTeal.withOpacity(0.1),
          borderRadius: BorderRadius.circular(12),
        ),
        child: Column(
          children: [
            Icon(icon, color: AppColors.corpusTeal, size: 32),
            const SizedBox(height: 8),
            Text(
              label,
              style: const TextStyle(
                fontWeight: FontWeight.w600,
                color: AppColors.corpusTeal,
              ),
            ),
          ],
        ),
      ),
    );
  }
}