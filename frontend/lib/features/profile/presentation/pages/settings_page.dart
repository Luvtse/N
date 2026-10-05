import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

import '../../../../core/theme/app_colors.dart';

class SettingsPage extends StatefulWidget {
  const SettingsPage({super.key});

  @override
  State<SettingsPage> createState() => _SettingsPageState();
}

class _SettingsPageState extends State<SettingsPage> {
  bool _notificationsEnabled = true;
  bool _emailNotifications = true;
  bool _pushNotifications = true;
  bool _darkMode = false;
  String _language = 'English';
  String _currency = 'USD';

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: AppColors.background,
      appBar: AppBar(
        title: const Text('Settings'),
        backgroundColor: Colors.white,
        elevation: 0,
      ),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          // Notifications section
          _buildSectionTitle('Notifications'),
          _buildSwitchTile(
            icon: Icons.notifications,
            title: 'Enable Notifications',
            subtitle: 'Receive notifications from NIDAW',
            value: _notificationsEnabled,
            onChanged: (value) => setState(() => _notificationsEnabled = value),
          ),
          _buildSwitchTile(
            icon: Icons.email,
            title: 'Email Notifications',
            subtitle: 'Receive email updates',
            value: _emailNotifications,
            onChanged: (value) => setState(() => _emailNotifications = value),
          ),
          _buildSwitchTile(
            icon: Icons.push_pin,
            title: 'Push Notifications',
            subtitle: 'Receive push notifications',
            value: _pushNotifications,
            onChanged: (value) => setState(() => _pushNotifications = value),
          ),

          const SizedBox(height: 24),

          // Appearance section
          _buildSectionTitle('Appearance'),
          _buildSwitchTile(
            icon: Icons.dark_mode,
            title: 'Dark Mode',
            subtitle: 'Use dark theme',
            value: _darkMode,
            onChanged: (value) => setState(() => _darkMode = value),
          ),

          const SizedBox(height: 24),

          // Language & Currency
          _buildSectionTitle('Regional'),
          _buildDropdownTile(
            icon: Icons.language,
            title: 'Language',
            value: _language,
            items: ['English', 'Spanish', 'French', 'German'],
            onChanged: (value) => setState(() => _language = value!),
          ),
          _buildDropdownTile(
            icon: Icons.attach_money,
            title: 'Currency',
            value: _currency,
            items: ['USD', 'EUR', 'GBP', 'JPY'],
            onChanged: (value) => setState(() => _currency = value!),
          ),

          const SizedBox(height: 24),

          // Account section
          _buildSectionTitle('Account'),
          _buildSettingsTile(
            icon: Icons.lock,
            title: 'Change Password',
            onTap: () {},
          ),
          _buildSettingsTile(
            icon: Icons.phone,
            title: 'Change Phone Number',
            onTap: () {},
          ),
          _buildSettingsTile(
            icon: Icons.email,
            title: 'Change Email',
            onTap: () {},
          ),

          const SizedBox(height: 24),

          // Privacy section
          _buildSectionTitle('Privacy & Security'),
          _buildSettingsTile(
            icon: Icons.privacy_tip,
            title: 'Privacy Settings',
            onTap: () => context.push('/settings/consent'),
          ),
          _buildSettingsTile(
            icon: Icons.security,
            title: 'Two-Factor Authentication',
            onTap: () {},
          ),
          _buildSettingsTile(
            icon: Icons.data_usage,
            title: 'Data Usage',
            onTap: () {},
          ),

          const SizedBox(height: 24),

          // About section
          _buildSectionTitle('About'),
          _buildSettingsTile(
            icon: Icons.info,
            title: 'About NIDAW',
            onTap: () {},
          ),
          _buildSettingsTile(
            icon: Icons.description,
            title: 'Terms of Service',
            onTap: () => context.push('/legal/terms_of_service'),
          ),
          _buildSettingsTile(
            icon: Icons.privacy_tip,
            title: 'Privacy Policy',
            onTap: () => context.push('/legal/privacy_policy'),
          ),

          const SizedBox(height: 24),

          // Danger zone
          _buildSectionTitle('Danger Zone'),
          _buildSettingsTile(
            icon: Icons.delete_forever,
            title: 'Delete Account',
            onTap: () => _showDeleteAccountDialog(),
            color: AppColors.error,
          ),
        ],
      ),
    );
  }

  Widget _buildSectionTitle(String title) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 12),
      child: Text(
        title,
        style: const TextStyle(
          fontSize: 18,
          fontWeight: FontWeight.bold,
          color: AppColors.textPrimary,
        ),
      ),
    );
  }

  Widget _buildSwitchTile({
    required IconData icon,
    required String title,
    required String subtitle,
    required bool value,
    required ValueChanged<bool> onChanged,
  }) {
    return Card(
      margin: const EdgeInsets.only(bottom: 8),
      child: SwitchListTile(
        secondary: Container(
          padding: const EdgeInsets.all(8),
          decoration: BoxDecoration(
            color: AppColors.primary.withOpacity(0.1),
            borderRadius: BorderRadius.circular(8),
          ),
          child: Icon(icon, color: AppColors.primary),
        ),
        title: Text(title, style: const TextStyle(fontWeight: FontWeight.w600)),
        subtitle: Text(subtitle, style: TextStyle(fontSize: 12, color: Colors.grey[600])),
        value: value,
        onChanged: onChanged,
        activeColor: AppColors.primary,
      ),
    );
  }

  Widget _buildDropdownTile({
    required IconData icon,
    required String title,
    required String value,
    required List<String> items,
    required ValueChanged<String?> onChanged,
  }) {
    return Card(
      margin: const EdgeInsets.only(bottom: 8),
      child: ListTile(
        leading: Container(
          padding: const EdgeInsets.all(8),
          decoration: BoxDecoration(
            color: AppColors.primary.withOpacity(0.1),
            borderRadius: BorderRadius.circular(8),
          ),
          child: Icon(icon, color: AppColors.primary),
        ),
        title: Text(title, style: const TextStyle(fontWeight: FontWeight.w600)),
        trailing: DropdownButton<String>(
          value: value,
          items: items.map((item) {
            return DropdownMenuItem(value: item, child: Text(item));
          }).toList(),
          onChanged: onChanged,
        ),
      ),
    );
  }

  Widget _buildSettingsTile({
    required IconData icon,
    required String title,
    required VoidCallback onTap,
    Color? color,
  }) {
    return Card(
      margin: const EdgeInsets.only(bottom: 8),
      child: ListTile(
        leading: Container(
          padding: const EdgeInsets.all(8),
          decoration: BoxDecoration(
            color: (color ?? AppColors.primary).withOpacity(0.1),
            borderRadius: BorderRadius.circular(8),
          ),
          child: Icon(icon, color: color ?? AppColors.primary),
        ),
        title: Text(
          title,
          style: TextStyle(
            fontWeight: FontWeight.w600,
            color: color ?? AppColors.textPrimary,
          ),
        ),
        trailing: Icon(Icons.chevron_right, color: color ?? Colors.grey),
        onTap: onTap,
      ),
    );
  }

  void _showDeleteAccountDialog() {
    showDialog(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Delete Account'),
        content: const Text(
          'Are you sure you want to delete your account? This action cannot be undone.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('Cancel'),
          ),
          ElevatedButton(
            onPressed: () {
              Navigator.pop(context);
              // TODO: Implement account deletion
            },
            style: ElevatedButton.styleFrom(backgroundColor: AppColors.error),
            child: const Text('Delete'),
          ),
        ],
      ),
    );
  }
}