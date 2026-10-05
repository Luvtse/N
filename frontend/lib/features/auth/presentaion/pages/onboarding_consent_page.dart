import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:go_router/go_router.dart';

import '../../../../core/theme/app_colors.dart';
import '../bloc/auth_bloc.dart';
import '../bloc/legal_consent_bloc.dart';

class OnboardingConsentPage extends StatefulWidget {
  const OnboardingConsentPage({super.key});

  @override
  State<OnboardingConsentPage> createState() => _OnboardingConsentPageState();
}

class _OnboardingConsentPageState extends State<OnboardingConsentPage> {
  bool _acceptTerms = false;
  bool _acceptPrivacy = false;
  bool _acceptCookies = false;
  bool _isLoading = false;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: AppColors.background,
      body: SafeArea(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              const SizedBox(height: 20),

              // Header
              _buildHeader(),

              const SizedBox(height: 32),

              // Terms of Service
              _buildConsentCheckbox(
                value: _acceptTerms,
                onChanged: (value) => setState(() => _acceptTerms = value ?? false),
                title: 'Terms of Service',
                description: 'I agree to the terms and conditions',
                onTap: () => _showDocument('terms_of_service', 'Terms of Service'),
                required: true,
              ),

              const SizedBox(height: 16),

              // Privacy Policy
              _buildConsentCheckbox(
                value: _acceptPrivacy,
                onChanged: (value) => setState(() => _acceptPrivacy = value ?? false),
                title: 'Privacy Policy',
                description: 'I agree to the privacy policy',
                onTap: () => _showDocument('privacy_policy', 'Privacy Policy'),
                required: true,
              ),

              const SizedBox(height: 16),

              // Cookie Policy
              _buildConsentCheckbox(
                value: _acceptCookies,
                onChanged: (value) => setState(() => _acceptCookies = value ?? false),
                title: 'Cookie Policy',
                description: 'I agree to the use of cookies',
                onTap: () => _showDocument('cookie_policy', 'Cookie Policy'),
                required: false,
              ),

              const Spacer(),

              // Continue button
              _buildContinueButton(),

              const SizedBox(height: 16),

              // Skip for now
              Center(
                child: TextButton(
                  onPressed: _isLoading ? null : () => context.go('/home'),
                  child: const Text('Skip for now'),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildHeader() {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Container(
          width: 64,
          height: 64,
          decoration: BoxDecoration(
            gradient: AppColors.primaryGradient,
            borderRadius: BorderRadius.circular(16),
          ),
          child: const Icon(
            Icons.verified_user,
            color: Colors.white,
            size: 32,
          ),
        ),
        const SizedBox(height: 24),
        const Text(
          'Welcome to NIDAW',
          style: TextStyle(
            fontSize: 28,
            fontWeight: FontWeight.bold,
            color: AppColors.textPrimary,
          ),
        ),
        const SizedBox(height: 8),
        Text(
          'Please review and accept our legal agreements to continue',
          style: TextStyle(
            fontSize: 16,
            color: Colors.grey[600],
          ),
        ),
      ],
    );
  }

  Widget _buildConsentCheckbox({
    required bool value,
    required ValueChanged<bool?> onChanged,
    required String title,
    required String description,
    required VoidCallback onTap,
    required bool required,
  }) {
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.circular(16),
        border: Border.all(
          color: value ? AppColors.primary : AppColors.border,
          width: value ? 2 : 1,
        ),
        boxShadow: [
          if (value)
            BoxShadow(
              color: AppColors.primary.withOpacity(0.1),
              blurRadius: 8,
              offset: const Offset(0, 2),
            ),
        ],
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Checkbox(
            value: value,
            onChanged: onChanged,
            activeColor: AppColors.primary,
            shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(4),
            ),
          ),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Text(
                      title,
                      style: const TextStyle(
                        fontSize: 16,
                        fontWeight: FontWeight.w600,
                        color: AppColors.textPrimary,
                      ),
                    ),
                    if (required) ...[
                      const SizedBox(width: 4),
                      const Text(
                        '*',
                        style: TextStyle(
                          color: AppColors.error,
                          fontSize: 16,
                          fontWeight: FontWeight.bold,
                        ),
                      ),
                    ],
                  ],
                ),
                const SizedBox(height: 4),
                GestureDetector(
                  onTap: onTap,
                  child: Text(
                    description,
                    style: const TextStyle(
                      fontSize: 14,
                      color: AppColors.primary,
                      decoration: TextDecoration.underline,
                    ),
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildContinueButton() {
    final canContinue = _acceptTerms && _acceptPrivacy;

    return SizedBox(
      height: 56,
      child: ElevatedButton(
        onPressed: canContinue && !_isLoading ? _continueToApp : null,
        style: ElevatedButton.styleFrom(
          backgroundColor: AppColors.primary,
          foregroundColor: Colors.white,
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(16),
          ),
          elevation: 0,
        ),
        child: _isLoading
            ? const SizedBox(
                width: 24,
                height: 24,
                child: CircularProgressIndicator(
                  strokeWidth: 2.5,
                  valueColor: AlwaysStoppedAnimation<Color>(Colors.white),
                ),
              )
            : const Text(
                'Continue',
                style: TextStyle(
                  fontSize: 16,
                  fontWeight: FontWeight.w600,
                ),
              ),
      ),
    );
  }

  Future<void> _showDocument(String type, String title) async {
    await context.push('/legal/$type');
  }

  Future<void> _continueToApp() async {
    setState(() => _isLoading = true);

    try {
      final consentBloc = context.read<LegalConsentBloc>();

      // Record consents
      if (_acceptTerms) {
        await consentBloc.giveConsent(
          documentType: 'terms_of_service',
          consentGiven: true,
          consentMethod: 'click_wrap',
        );
      }

      if (_acceptPrivacy) {
        await consentBloc.giveConsent(
          documentType: 'privacy_policy',
          consentGiven: true,
          consentMethod: 'click_wrap',
        );
      }

      if (_acceptCookies) {
        await consentBloc.giveConsent(
          documentType: 'cookie_policy',
          consentGiven: true,
          consentMethod: 'click_wrap',
        );
      }

      // Mark onboarding as complete
      context.read<AuthBloc>().add(const OnboardingCompleted());

      if (mounted) {
        context.go('/home');
      }
    } catch (e) {
      setState(() => _isLoading = false);
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Error: $e'),
            backgroundColor: AppColors.error,
          ),
        );
      }
    }
  }
}