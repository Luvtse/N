import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:flutter_html/flutter_html.dart';
import 'package:go_router/go_router.dart';
import '../../../../core/theme/app_colors.dart';
import '../bloc/legal_consent_bloc.dart';

class LegalConsentPage extends StatefulWidget {
  final String documentType;
  final String documentTitle;
  
  const LegalConsentPage({
    super.key,
    required this.documentType,
    required this.documentTitle,
  });

  @override
  State<LegalConsentPage> createState() => _LegalConsentPageState();
}

class _LegalConsentPageState extends State<LegalConsentPage> {
  bool _hasConsented = false;
  bool _isLoading = true;
  String _documentContent = '';
  String _documentVersion = '';

  @override
  void initState() {
    super.initState();
    _loadDocument();
  }

  Future<void> _loadDocument() async {
    try {
      final document = await context.read<LegalConsentBloc>().getDocument(
        widget.documentType,
      );
      
      setState(() {
        _documentContent = document.contentHtml ?? document.content;
        _documentVersion = document.version;
        _isLoading = false;
      });
    } catch (e) {
      setState(() => _isLoading = false);
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Failed to load document: $e')),
        );
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Text(widget.documentTitle),
        leading: IconButton(
          icon: const Icon(Icons.arrow_back),
          onPressed: () => context.pop(),
        ),
      ),
      body: _isLoading
          ? const Center(child: CircularProgressIndicator())
          : Column(
              children: [
                // Document Content (Scrollable)
                Expanded(
                  child: SingleChildScrollView(
                    padding: const EdgeInsets.all(16),
                    child: Html(
                      data: _documentContent,
                      style: {
                        "body": Style(
                          fontSize: FontSize(14),
                          lineHeight: LineHeight(1.6),
                        ),
                        "h1": Style(fontSize: FontSize(20), fontWeight: FontWeight.bold),
                        "h2": Style(fontSize: FontSize(18), fontWeight: FontWeight.bold),
                        "h3": Style(fontSize: FontSize(16), fontWeight: FontWeight.bold),
                      },
                    ),
                  ),
                ),
                
                // Consent Checkbox
                Container(
                  padding: const EdgeInsets.all(16),
                  decoration: BoxDecoration(
                    color: Colors.white,
                    boxShadow: [
                      BoxShadow(
                        color: Colors.black.withOpacity(0.05),
                        blurRadius: 10,
                        offset: const Offset(0, -2),
                      ),
                    ],
                  ),
                  child: Column(
                    children: [
                      // Checkbox
                      Row(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Checkbox(
                            value: _hasConsented,
                            onChanged: (value) {
                              setState(() => _hasConsented = value ?? false);
                            },
                            activeColor: AppColors.primary,
                          ),
                          Expanded(
                            child: GestureDetector(
                              onTap: () {
                                setState(() => _hasConsented = !_hasConsented);
                              },
                              child: Text(
                                'I have read and agree to the ${widget.documentTitle} (Version $_documentVersion)',
                                style: const TextStyle(fontSize: 14),
                              ),
                            ),
                          ),
                        ],
                      ),
                      
                      const SizedBox(height: 16),
                      
                      // Action Buttons
                      Row(
                        children: [
                          Expanded(
                            child: OutlinedButton(
                              onPressed: () => context.pop(),
                              child: const Text('Cancel'),
                            ),
                          ),
                          const SizedBox(width: 12),
                          Expanded(
                            child: ElevatedButton(
                              onPressed: _hasConsented
                                  ? () => _giveConsent()
                                  : null,
                              child: const Text('I Agree'),
                            ),
                          ),
                        ],
                      ),
                    ],
                  ),
                ),
              ],
            ),
    );
  }

  Future<void> _giveConsent() async {
    final bloc = context.read<LegalConsentBloc>();
    
    await bloc.giveConsent(
      documentType: widget.documentType,
      consentGiven: true,
      consentMethod: 'click_wrap',
    );
    
    if (mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(
          content: Text('Consent recorded successfully'),
          backgroundColor: AppColors.success,
        ),
      );
      
      // Navigate to next step or home
      context.go('/home');
    }
  }
}