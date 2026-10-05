import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:equatable/equatable.dart';
import '../../../../core/network/api_client.dart';

// Events
abstract class LegalConsentEvent extends Equatable {
  @override
  List<Object?> get props => [];
}

class LoadDocument extends LegalConsentEvent {
  final String documentType;
  LoadDocument(this.documentType);
  
  @override
  List<Object?> get props => [documentType];
}

class GiveConsent extends LegalConsentEvent {
  final String documentType;
  final bool consentGiven;
  final String consentMethod;
  
  GiveConsent({
    required this.documentType,
    required this.consentGiven,
    required this.consentMethod,
  });
  
  @override
  List<Object?> get props => [documentType, consentGiven, consentMethod];
}

class WithdrawConsent extends LegalConsentEvent {
  final String documentType;
  final String reason;
  
  WithdrawConsent({
    required this.documentType,
    required this.reason,
  });
  
  @override
  List<Object?> get props => [documentType, reason];
}

class LoadUserConsents extends LegalConsentEvent {}

// States
abstract class LegalConsentState extends Equatable {
  @override
  List<Object?> get props => [];
}

class LegalConsentInitial extends LegalConsentState {}
class LegalConsentLoading extends LegalConsentState {}
class LegalConsentLoaded extends LegalConsentState {
  final LegalDocument document;
  LegalConsentLoaded(this.document);
  
  @override
  List<Object?> get props => [document];
}

class ConsentGiven extends LegalConsentState {}
class ConsentWithdrawn extends LegalConsentState {}

class UserConsentsLoaded extends LegalConsentState {
  final List<ConsentStatus> consents;
  UserConsentsLoaded(this.consents);
  
  @override
  List<Object?> get props => [consents];
}

class LegalConsentError extends LegalConsentState {
  final String message;
  LegalConsentError(this.message);
  
  @override
  List<Object?> get props => [message];
}

// Models
class LegalDocument {
  final String id;
  final String documentType;
  final String version;
  final String title;
  final String content;
  final String? contentHtml;
  final String language;
  final String region;
  final DateTime effectiveDate;
  final bool isActive;
  final bool requiresReconsent;
  
  LegalDocument({
    required this.id,
    required this.documentType,
    required this.version,
    required this.title,
    required this.content,
    this.contentHtml,
    required this.language,
    required this.region,
    required this.effectiveDate,
    required this.isActive,
    required this.requiresReconsent,
  });
  
  factory LegalDocument.fromJson(Map<String, dynamic> json) {
    return LegalDocument(
      id: json['id'],
      documentType: json['document_type'],
      version: json['version'],
      title: json['title'],
      content: json['content'],
      contentHtml: json['content_html'],
      language: json['language'],
      region: json['region'],
      effectiveDate: DateTime.parse(json['effective_date']),
      isActive: json['is_active'],
      requiresReconsent: json['requires_reconsent'],
    );
  }
}

class ConsentStatus {
  final String documentType;
  final String documentVersion;
  final bool consentGiven;
  final DateTime consentDate;
  final bool requiresUpdate;
  
  ConsentStatus({
    required this.documentType,
    required this.documentVersion,
    required this.consentGiven,
    required this.consentDate,
    required this.requiresUpdate,
  });
  
  factory ConsentStatus.fromJson(Map<String, dynamic> json) {
    return ConsentStatus(
      documentType: json['document_type'],
      documentVersion: json['document_version'],
      consentGiven: json['consent_given'],
      consentDate: DateTime.parse(json['consent_date']),
      requiresUpdate: json['requires_update'],
    );
  }
}

// BLoC
class LegalConsentBloc extends Bloc<LegalConsentEvent, LegalConsentState> {
  final ApiClient _apiClient;
  
  LegalConsentBloc(this._apiClient) : super(LegalConsentInitial()) {
    on<LoadDocument>(_onLoadDocument);
    on<GiveConsent>(_onGiveConsent);
    on<WithdrawConsent>(_onWithdrawConsent);
    on<LoadUserConsents>(_onLoadUserConsents);
  }
  
  Future<LegalDocument> getDocument(String documentType) async {
    final response = await _apiClient.get(
      '/api/v1/legal/documents',
      queryParameters: {'type': documentType},
    );
    
    return LegalDocument.fromJson(response.data);
  }
  
  Future<void> _onLoadDocument(
    LoadDocument event,
    Emitter<LegalConsentState> emit,
  ) async {
    emit(LegalConsentLoading());
    
    try {
      final document = await getDocument(event.documentType);
      emit(LegalConsentLoaded(document));
    } catch (e) {
      emit(LegalConsentError(e.toString()));
    }
  }
  
  Future<void> giveConsent({
    required String documentType,
    required bool consentGiven,
    required String consentMethod,
  }) async {
    await _apiClient.post(
      '/api/v1/consent',
      data: {
        'document_type': documentType,
        'consent_given': consentGiven,
        'consent_method': consentMethod,
      },
    );
  }
  
  Future<void> _onGiveConsent(
    GiveConsent event,
    Emitter<LegalConsentState> emit,
  ) async {
    emit(LegalConsentLoading());
    
    try {
      await giveConsent(
        documentType: event.documentType,
        consentGiven: event.consentGiven,
        consentMethod: event.consentMethod,
      );
      emit(ConsentGiven());
    } catch (e) {
      emit(LegalConsentError(e.toString()));
    }
  }
  
  Future<void> _onWithdrawConsent(
    WithdrawConsent event,
    Emitter<LegalConsentState> emit,
  ) async {
    emit(LegalConsentLoading());
    
    try {
      await _apiClient.post(
        '/api/v1/consent/withdraw',
        queryParameters: {
          'type': event.documentType,
          'reason': event.reason,
        },
      );
      emit(ConsentWithdrawn());
    } catch (e) {
      emit(LegalConsentError(e.toString()));
    }
  }
  
  Future<void> _onLoadUserConsents(
    LoadUserConsents event,
    Emitter<LegalConsentState> emit,
  ) async {
    emit(LegalConsentLoading());
    
    try {
      final response = await _apiClient.get('/api/v1/consent');
      final consents = (response.data as List)
          .map((json) => ConsentStatus.fromJson(json))
          .toList();
      emit(UserConsentsLoaded(consents));
    } catch (e) {
      emit(LegalConsentError(e.toString()));
    }
  }
}