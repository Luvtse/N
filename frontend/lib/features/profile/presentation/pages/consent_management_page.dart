import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import '../../../../core/theme/app_colors.dart';
import '../../auth/presentation/bloc/legal_consent_bloc.dart';

class ConsentManagementPage extends StatefulWidget {
  const ConsentManagementPage({super.key});

  @override
  State<ConsentManagementPage> createState() => _ConsentManagementPageState();
}

class _ConsentManagementPageState extends State<ConsentManagementPage> {
  @override
  void initState() {
    super.initState();
    context.read<LegalConsentBloc>().add(LoadUserConsents());
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Privacy & Consent'),
      ),
      body: BlocBuilder<LegalConsentBloc, LegalConsentState>(
        builder: (context, state) {
          if (state is LegalConsentLoading) {
            return const Center(child: CircularProgressIndicator());
          }
          
          if (state is UserConsentsLoaded) {
            return ListView(
              padding: const EdgeInsets.all(16),
              children: [
                // Header
                const Text(
                  'Your Consent Preferences',
                  style: TextStyle(
                    fontSize: 20,
                    fontWeight: FontWeight.bold,
                  ),
                ),
                const SizedBox(height: 8),
                Text(
                  'Manage your consent for legal documents and data processing',
                  style: TextStyle(
                    fontSize: 14,
                    color: Colors.grey[600],
                  ),
                ),
                
                const SizedBox(height: 24),
                
                // Consent List
                ...state.consents.map((consent) => _buildConsentCard(consent)),
                
                const SizedBox(height: 24),
                
                // Info Box
                Container(
                  padding: const EdgeInsets.all(16),
                  decoration: BoxDecoration(
                    color: AppColors.info.withOpacity(0.1),
                    borderRadius: BorderRadius.circular(12),
                    border: Border.all(color: AppColors.info.withOpacity(0.3)),
                  ),
                  child: Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      const Icon(Icons.info_outline, color: AppColors.info),
                      const SizedBox(width: 12),
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            const Text(
                              'About Your Data',
                              style: TextStyle(
                                fontWeight: FontWeight.w600,
                                fontSize: 14,
                              ),
                            ),
                            const SizedBox(height: 4),
                            Text(
                              'You can withdraw consent at any time. Some features may be limited if you withdraw essential consents.',
                              style: TextStyle(
                                fontSize: 12,
                                color: Colors.grey[700],
                              ),
                            ),
                          ],
                        ),
                      ),
                    ],
                  ),
                ),
              ],
            );
          }
          
          if (state is LegalConsentError) {
            return Center(
              child: Column(
                mainAxisAlignment: MainAxisAlignment.center,
                children: [
                  const Icon(Icons.error_outline, size: 48, color: AppColors.error),
                  const SizedBox(height: 16),
                  Text('Error: ${state.message}'),
                  const SizedBox(height: 16),
                  ElevatedButton(
                    onPressed: () {
                      context.read<LegalConsentBloc>().add(LoadUserConsents());
                    },
                    child: const Text('Retry'),
                  ),
                ],
              ),
            );
          }
          
          return const Center(child: Text('No consent data available'));
        },
      ),
    );
  }

  Widget _buildConsentCard(ConsentStatus consent) {
    return Card(
      margin: const EdgeInsets.only(bottom: 12),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(
                  consent.consentGiven ? Icons.check_circle : Icons.cancel,
                  color: consent.consentGiven ? AppColors.success : AppColors.error,
                  size: 24,
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        _getDocumentTitle(consent.documentType),
                        style: const TextStyle(
                          fontSize: 16,
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                      const SizedBox(height: 4),
                      Text(
                        'Version ${consent.documentVersion}',
                        style: TextStyle(
                          fontSize: 12,
                          color: Colors.grey[600],
                        ),
                      ),
                    ],
                  ),
                ),
                if (consent.requiresUpdate)
                  Container(
                    padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                    decoration: BoxDecoration(
                      color: AppColors.warning,
                      borderRadius: BorderRadius.circular(12),
                    ),
                    child: const Text(
                      'Update Required',
                      style: TextStyle(
                        fontSize: 10,
                        color: Colors.white,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                  ),
              ],
            ),
            
            const SizedBox(height: 12),
            
            Row(
              children: [
                Text(
                  'Consented on: ${_formatDate(consent.consentDate)}',
                  style: TextStyle(
                    fontSize: 12,
                    color: Colors.grey[600],
                  ),
                ),
                const Spacer(),
                if (consent.consentGiven)
                  TextButton(
                    onPressed: () => _showWithdrawDialog(consent.documentType),
                    child: const Text(
                      'Withdraw',
                      style: TextStyle(color: AppColors.error),
                    ),
                  ),
                if (!consent.consentGiven)
                  TextButton(
                    onPressed: () => _giveConsent(consent.documentType),
                    child: const Text('Give Consent'),
                  ),
              ],
            ),
          ],
        ),
      ),
    );
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
        return documentType;
    }
  }

  String _formatDate(DateTime date) {
    return '${date.day}/${date.month}/${date.year}';
  }

  Future<void> _showWithdrawDialog(String documentType) async {
    final reasonController = TextEditingController();
    
    return showDialog(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Withdraw Consent'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text(
              'Are you sure you want to withdraw consent? Some features may be limited.',
              style: TextStyle(fontSize: 14),
            ),
            const SizedBox(height: 16),
            TextField(
              controller: reasonController,
              decoration: const InputDecoration(
                labelText: 'Reason (optional)',
                border: OutlineInputBorder(),
              ),
              maxLines: 3,
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('Cancel'),
          ),
          ElevatedButton(
            onPressed: () {
              Navigator.pop(context);
              _withdrawConsent(documentType, reasonController.text);
            },
            style: ElevatedButton.styleFrom(
              backgroundColor: AppColors.error,
            ),
            child: const Text('Withdraw'),
          ),
        ],
      ),
    );
  }

  Future<void> _withdrawConsent(String documentType, String reason) async {
    final bloc = context.read<LegalConsentBloc>();
    
    await bloc.add(WithdrawConsent(
      documentType: documentType,
      reason: reason,
    ));
    
    if (mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(
          content: Text('Consent withdrawn successfully'),
          backgroundColor: AppColors.success,
        ),
      );
      
      // Reload consents
      bloc.add(LoadUserConsents());
    }
  }

  Future<void> _giveConsent(String documentType) async {
    final bloc = context.read<LegalConsentBloc>();
    
    await bloc.giveConsent(
      documentType: documentType,
      consentGiven: true,
      consentMethod: 'click_wrap',
    );
    
    if (mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(
          content: Text('Consent given successfully'),
          backgroundColor: AppColors.success,
        ),
      );
      
      // Reload consents
      bloc.add(LoadUserConsents());
    }
  }
}