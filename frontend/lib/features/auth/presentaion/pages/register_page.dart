import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:go_router/go_router.dart';

import '../../../../core/theme/app_colors.dart';
import '../bloc/auth_bloc.dart';

class RegisterPage extends StatefulWidget {
  const RegisterPage({super.key});

  @override
  State<RegisterPage> createState() => _RegisterPageState();
}

class _RegisterPageState extends State<RegisterPage> {
  final _formKey = GlobalKey<FormState>();
  final _emailController = TextEditingController();
  final _passwordController = TextEditingController();
  final _fullNameController = TextEditingController();
  final _phoneController = TextEditingController();

  final _emailFocusNode = FocusNode();
  final _passwordFocusNode = FocusNode();
  final _fullNameFocusNode = FocusNode();
  final _phoneFocusNode = FocusNode();

  bool _obscurePassword = true;
  bool _acceptTerms = false;

  @override
  void dispose() {
    _emailController.dispose();
    _passwordController.dispose();
    _fullNameController.dispose();
    _phoneController.dispose();
    _emailFocusNode.dispose();
    _passwordFocusNode.dispose();
    _fullNameFocusNode.dispose();
    _phoneFocusNode.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Create Account'),
        leading: IconButton(
          icon: const Icon(Icons.arrow_back),
          onPressed: () => context.pop(),
        ),
      ),
      body: BlocConsumer<AuthBloc, AuthState>(
        listener: _handleAuthState,
        builder: (context, state) {
          final isLoading = state is AuthLoading;

          return SingleChildScrollView(
            padding: const EdgeInsets.all(24),
            child: Form(
              key: _formKey,
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  const SizedBox(height: 16),
                  _buildHeader(),
                  const SizedBox(height: 32),
                  _buildFullNameField(isLoading: isLoading),
                  const SizedBox(height: 16),
                  _buildEmailField(isLoading: isLoading),
                  const SizedBox(height: 16),
                  _buildPhoneField(isLoading: isLoading),
                  const SizedBox(height: 16),
                  _buildPasswordField(isLoading: isLoading),
                  const SizedBox(height: 8),
                  _buildPasswordStrengthIndicator(),
                  const SizedBox(height: 24),
                  _buildTermsCheckbox(),
                  const SizedBox(height: 24),
                  _buildRegisterButton(isLoading: isLoading),
                  const SizedBox(height: 16),
                  _buildLoginLink(),
                  if (state is AuthError) ...[
                    const SizedBox(height: 24),
                    _buildErrorMessage(state.message),
                  ],
                ],
              ),
            ),
          );
        },
      ),
    );
  }

  // ==========================================================================
  // HEADER
  // ==========================================================================

  Widget _buildHeader() {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const Text(
          'Join NIDAW',
          style: TextStyle(
            fontSize: 28,
            fontWeight: FontWeight.bold,
          ),
        ),
        const SizedBox(height: 8),
        Text(
          'Create your account to get started',
          style: TextStyle(
            fontSize: 16,
            color: Colors.grey[600],
          ),
        ),
      ],
    );
  }

  // ==========================================================================
  // FORM FIELDS
  // ==========================================================================

  Widget _buildFullNameField({required bool isLoading}) {
    return TextFormField(
      controller: _fullNameController,
      focusNode: _fullNameFocusNode,
      textInputAction: TextInputAction.next,
      enabled: !isLoading,
      decoration: const InputDecoration(
        labelText: 'Full Name',
        hintText: 'John Doe',
        prefixIcon: Icon(Icons.person_outlined),
      ),
      validator: (value) {
        if (value == null || value.trim().isEmpty) {
          return 'Full name is required';
        }
        if (value.trim().length < 2) {
          return 'Full name must be at least 2 characters';
        }
        return null;
      },
      onFieldSubmitted: (_) => _emailFocusNode.requestFocus(),
    );
  }

  Widget _buildEmailField({required bool isLoading}) {
    return TextFormField(
      controller: _emailController,
      focusNode: _emailFocusNode,
      keyboardType: TextInputType.emailAddress,
      textInputAction: TextInputAction.next,
      autocorrect: false,
      enabled: !isLoading,
      decoration: const InputDecoration(
        labelText: 'Email',
        hintText: 'you@example.com',
        prefixIcon: Icon(Icons.email_outlined),
      ),
      validator: (value) {
        if (value == null || value.trim().isEmpty) {
          return 'Email is required';
        }
        final regex = RegExp(r'^[\w-\.]+@([\w-]+\.)+[\w-]{2,4}$');
        if (!regex.hasMatch(value.trim())) {
          return 'Please enter a valid email';
        }
        return null;
      },
      onFieldSubmitted: (_) => _phoneFocusNode.requestFocus(),
    );
  }

  Widget _buildPhoneField({required bool isLoading}) {
    return TextFormField(
      controller: _phoneController,
      focusNode: _phoneFocusNode,
      keyboardType: TextInputType.phone,
      textInputAction: TextInputAction.next,
      enabled: !isLoading,
      decoration: const InputDecoration(
        labelText: 'Phone Number',
        hintText: '+1 234 567 8900',
        prefixIcon: Icon(Icons.phone_outlined),
      ),
      validator: (value) {
        if (value == null || value.trim().isEmpty) {
          return 'Phone number is required';
        }
        final regex = RegExp(r'^\+?[\d\s\-\(\)]{7,20}$');
        if (!regex.hasMatch(value.trim())) {
          return 'Please enter a valid phone number';
        }
        return null;
      },
      onFieldSubmitted: (_) => _passwordFocusNode.requestFocus(),
    );
  }

  Widget _buildPasswordField({required bool isLoading}) {
    return TextFormField(
      controller: _passwordController,
      focusNode: _passwordFocusNode,
      obscureText: _obscurePassword,
      textInputAction: TextInputAction.done,
      enabled: !isLoading,
      decoration: InputDecoration(
        labelText: 'Password',
        hintText: 'Create a strong password',
        prefixIcon: const Icon(Icons.lock_outlined),
        suffixIcon: IconButton(
          icon: Icon(
            _obscurePassword
                ? Icons.visibility_outlined
                : Icons.visibility_off_outlined,
          ),
          onPressed: () => setState(() => _obscurePassword = !_obscurePassword),
        ),
      ),
      validator: (value) {
        if (value == null || value.isEmpty) {
          return 'Password is required';
        }
        if (value.length < 8) {
          return 'Password must be at least 8 characters';
        }
        if (!value.contains(RegExp(r'[A-Z]'))) {
          return 'Password must contain an uppercase letter';
        }
        if (!value.contains(RegExp(r'[a-z]'))) {
          return 'Password must contain a lowercase letter';
        }
        if (!value.contains(RegExp(r'[0-9]'))) {
          return 'Password must contain a number';
        }
        return null;
      },
      onFieldSubmitted: (_) => _submitRegistration(),
      onChanged: (_) => setState(() {}), // Update password strength
    );
  }

  // ==========================================================================
  // PASSWORD STRENGTH INDICATOR
  // ==========================================================================

  Widget _buildPasswordStrengthIndicator() {
    final password = _passwordController.text;
    final strength = _calculatePasswordStrength(password);

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        LinearProgressIndicator(
          value: strength.score,
          backgroundColor: Colors.grey[200],
          valueColor: AlwaysStoppedAnimation<Color>(strength.color),
          minHeight: 6,
        ),
        const SizedBox(height: 8),
        Text(
          'Password strength: ${strength.label}',
          style: TextStyle(
            fontSize: 12,
            color: strength.color,
            fontWeight: FontWeight.w500,
          ),
        ),
      ],
    );
  }

  _PasswordStrength _calculatePasswordStrength(String password) {
    if (password.isEmpty) {
      return _PasswordStrength(0.0, Colors.grey, 'Enter a password');
    }

    int score = 0;

    // Length check
    if (password.length >= 8) score++;
    if (password.length >= 12) score++;

    // Character variety
    if (password.contains(RegExp(r'[A-Z]'))) score++;
    if (password.contains(RegExp(r'[a-z]'))) score++;
    if (password.contains(RegExp(r'[0-9]'))) score++;
    if (password.contains(RegExp(r'[!@#$%^&*(),.?":{}|<>]'))) score++;

    if (score <= 2) {
      return _PasswordStrength(0.33, Colors.red, 'Weak');
    } else if (score <= 4) {
      return _PasswordStrength(0.66, Colors.orange, 'Medium');
    } else {
      return _PasswordStrength(1.0, Colors.green, 'Strong');
    }
  }

  // ==========================================================================
  // TERMS & BUTTONS
  // ==========================================================================

  Widget _buildTermsCheckbox() {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Checkbox(
          value: _acceptTerms,
          onChanged: (value) => setState(() => _acceptTerms = value ?? false),
          activeColor: AppColors.primary,
        ),
        Expanded(
          child: GestureDetector(
            onTap: () => setState(() => _acceptTerms = !_acceptTerms),
            child: Padding(
              padding: const EdgeInsets.only(top: 12),
              child: Text.rich(
                TextSpan(
                  text: 'I agree to the ',
                  style: TextStyle(fontSize: 14, color: Colors.grey[700]),
                  children: [
                    TextSpan(
                      text: 'Terms of Service',
                      style: const TextStyle(
                        color: AppColors.primary,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                    const TextSpan(text: ' and '),
                    TextSpan(
                      text: 'Privacy Policy',
                      style: const TextStyle(
                        color: AppColors.primary,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
      ],
    );
  }

  Widget _buildRegisterButton({required bool isLoading}) {
    return SizedBox(
      height: 52,
      child: ElevatedButton(
        onPressed: isLoading || !_acceptTerms ? null : _submitRegistration,
        style: ElevatedButton.styleFrom(
          backgroundColor: AppColors.primary,
          foregroundColor: Colors.white,
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(12),
          ),
        ),
        child: isLoading
            ? const SizedBox(
                width: 24,
                height: 24,
                child: CircularProgressIndicator(
                  strokeWidth: 2.5,
                  valueColor: AlwaysStoppedAnimation<Color>(Colors.white),
                ),
              )
            : const Text(
                'Create Account',
                style: TextStyle(
                  fontSize: 16,
                  fontWeight: FontWeight.w600,
                ),
              ),
      ),
    );
  }

  Widget _buildLoginLink() {
    return Row(
      mainAxisAlignment: MainAxisAlignment.center,
      children: [
        Text(
          'Already have an account? ',
          style: TextStyle(color: Colors.grey[600]),
        ),
        GestureDetector(
          onTap: () => context.pop(),
          child: const Text(
            'Sign In',
            style: TextStyle(
              color: AppColors.primary,
              fontWeight: FontWeight.w600,
            ),
          ),
        ),
      ],
    );
  }

  Widget _buildErrorMessage(String message) {
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: AppColors.error.withOpacity(0.1),
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: AppColors.error.withOpacity(0.3)),
      ),
      child: Row(
        children: [
          const Icon(Icons.error_outline, color: AppColors.error, size: 20),
          const SizedBox(width: 12),
          Expanded(
            child: Text(
              message,
              style: const TextStyle(
                color: AppColors.error,
                fontSize: 14,
              ),
            ),
          ),
        ],
      ),
    );
  }

  // ==========================================================================
  // ACTIONS
  // ==========================================================================

  void _submitRegistration() {
    if (!_formKey.currentState!.validate()) {
      return;
    }

    if (!_acceptTerms) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(
          content: Text('Please accept the Terms of Service and Privacy Policy'),
          backgroundColor: AppColors.warning,
        ),
      );
      return;
    }

    // Dismiss keyboard
    FocusScope.of(context).unfocus();

    context.read<AuthBloc>().add(
          RegisterRequested(
            email: _emailController.text.trim(),
            password: _passwordController.text,
            fullName: _fullNameController.text.trim(),
            phone: _phoneController.text.trim(),
          ),
        );
  }

  void _handleAuthState(BuildContext context, AuthState state) {
    if (state is AuthAuthenticated) {
      // New users always go to onboarding
      context.go('/onboarding');
    }
  }
}

class _PasswordStrength {
  final double score;
  final Color color;
  final String label;

  _PasswordStrength(this.score, this.color, this.label);
}