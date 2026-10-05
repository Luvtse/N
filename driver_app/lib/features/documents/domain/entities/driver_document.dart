import 'package:equatable/equatable.dart';

class DriverDocument extends Equatable {
  final String id;
  final String driverId;
  final String type;
  final String name;
  final String? documentNumber;
  final DateTime? issueDate;
  final DateTime? expiryDate;
  final String status;
  final String? fileUrl;
  final String? verificationNotes;
  final DateTime createdAt;
  final DateTime updatedAt;

  const DriverDocument({
    required this.id,
    required this.driverId,
    required this.type,
    required this.name,
    this.documentNumber,
    this.issueDate,
    this.expiryDate,
    required this.status,
    this.fileUrl,
    this.verificationNotes,
    required this.createdAt,
    required this.updatedAt,
  });

  factory DriverDocument.fromJson(Map<String, dynamic> json) {
    return DriverDocument(
      id: json['id'] as String,
      driverId: json['driver_id'] as String,
      type: json['type'] as String,
      name: json['name'] as String,
      documentNumber: json['document_number'] as String?,
      issueDate: json['issue_date'] != null
          ? DateTime.parse(json['issue_date'] as String)
          : null,
      expiryDate: json['expiry_date'] != null
          ? DateTime.parse(json['expiry_date'] as String)
          : null,
      status: json['status'] as String,
      fileUrl: json['file_url'] as String?,
      verificationNotes: json['verification_notes'] as String?,
      createdAt: DateTime.parse(json['created_at'] as String),
      updatedAt: DateTime.parse(json['updated_at'] as String),
    );
  }

  Map<String, dynamic> toJson() {
    return {
      'id': id,
      'driver_id': driverId,
      'type': type,
      'name': name,
      'document_number': documentNumber,
      'issue_date': issueDate?.toIso8601String(),
      'expiry_date': expiryDate?.toIso8601String(),
      'status': status,
      'file_url': fileUrl,
      'verification_notes': verificationNotes,
      'created_at': createdAt.toIso8601String(),
      'updated_at': updatedAt.toIso8601String(),
    };
  }

  bool get isVerified => status == 'verified';
  bool get isPending => status == 'pending';
  bool get isRejected => status == 'rejected';
  bool get isExpired => expiryDate != null && expiryDate!.isBefore(DateTime.now());

  @override
  List<Object?> get props => [
        id,
        driverId,
        type,
        name,
        status,
        expiryDate,
      ];
}