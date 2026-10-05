import 'package:equatable/equatable.dart';

class DriverStats extends Equatable {
  final double todayEarnings;
  final int todayRides;
  final double weeklyEarnings;
  final int weeklyRides;
  final double monthlyEarnings;
  final int monthlyRides;
  final double averageRating;
  final double acceptanceRate;
  final double completionRate;
  final int onlineHours;
  final double averageFare;
  final double tips;
  final int cancellations;
  final DateTime lastUpdated;

  const DriverStats({
    required this.todayEarnings,
    required this.todayRides,
    required this.weeklyEarnings,
    required this.weeklyRides,
    required this.monthlyEarnings,
    required this.monthlyRides,
    required this.averageRating,
    required this.acceptanceRate,
    required this.completionRate,
    required this.onlineHours,
    required this.averageFare,
    required this.tips,
    required this.cancellations,
    required this.lastUpdated,
  });

  factory DriverStats.fromJson(Map<String, dynamic> json) {
    return DriverStats(
      todayEarnings: (json['today_earnings'] as num).toDouble(),
      todayRides: json['today_rides'] as int,
      weeklyEarnings: (json['weekly_earnings'] as num).toDouble(),
      weeklyRides: json['weekly_rides'] as int,
      monthlyEarnings: (json['monthly_earnings'] as num).toDouble(),
      monthlyRides: json['monthly_rides'] as int,
      averageRating: (json['average_rating'] as num).toDouble(),
      acceptanceRate: (json['acceptance_rate'] as num).toDouble(),
      completionRate: (json['completion_rate'] as num).toDouble(),
      onlineHours: (json['online_hours'] as num).toDouble(),
      averageFare: (json['average_fare'] as num).toDouble(),
      tips: (json['tips'] as num).toDouble(),
      cancellations: json['cancellations'] as int,
      lastUpdated: DateTime.parse(json['last_updated'] as String),
    );
  }

  Map<String, dynamic> toJson() {
    return {
      'today_earnings': todayEarnings,
      'today_rides': todayRides,
      'weekly_earnings': weeklyEarnings,
      'weekly_rides': weeklyRides,
      'monthly_earnings': monthlyEarnings,
      'monthly_rides': monthlyRides,
      'average_rating': averageRating,
      'acceptance_rate': acceptanceRate,
      'completion_rate': completionRate,
      'online_hours': onlineHours,
      'average_fare': averageFare,
      'tips': tips,
      'cancellations': cancellations,
      'last_updated': lastUpdated.toIso8601String(),
    };
  }

  @override
  List<Object?> get props => [
        todayEarnings,
        todayRides,
        weeklyEarnings,
        weeklyRides,
        monthlyEarnings,
        monthlyRides,
        averageRating,
        acceptanceRate,
        completionRate,
        onlineHours,
      ];
}