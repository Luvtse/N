import 'package:equatable/equatable.dart';

class EarningsReport extends Equatable {
  final String period;
  final DateTime startDate;
  final DateTime endDate;
  final double totalEarnings;
  final double baseFares;
  final double tips;
  final double bonuses;
  final double deductions;
  final int totalRides;
  final int completedRides;
  final int cancelledRides;
  final double averageFare;
  final double averageTip;
  final double onlineHours;
  final double earningsPerHour;
  final List<DailyEarnings> dailyBreakdown;

  const EarningsReport({
    required this.period,
    required this.startDate,
    required this.endDate,
    required this.totalEarnings,
    required this.baseFares,
    required this.tips,
    required this.bonuses,
    required this.deductions,
    required this.totalRides,
    required this.completedRides,
    required this.cancelledRides,
    required this.averageFare,
    required this.averageTip,
    required this.onlineHours,
    required this.earningsPerHour,
    required this.dailyBreakdown,
  });

  factory EarningsReport.fromJson(Map<String, dynamic> json) {
    return EarningsReport(
      period: json['period'] as String,
      startDate: DateTime.parse(json['start_date'] as String),
      endDate: DateTime.parse(json['end_date'] as String),
      totalEarnings: (json['total_earnings'] as num).toDouble(),
      baseFares: (json['base_fares'] as num).toDouble(),
      tips: (json['tips'] as num).toDouble(),
      bonuses: (json['bonuses'] as num).toDouble(),
      deductions: (json['deductions'] as num).toDouble(),
      totalRides: json['total_rides'] as int,
      completedRides: json['completed_rides'] as int,
      cancelledRides: json['cancelled_rides'] as int,
      averageFare: (json['average_fare'] as num).toDouble(),
      averageTip: (json['average_tip'] as num).toDouble(),
      onlineHours: (json['online_hours'] as num).toDouble(),
      earningsPerHour: (json['earnings_per_hour'] as num).toDouble(),
      dailyBreakdown: (json['daily_breakdown'] as List)
          .map((e) => DailyEarnings.fromJson(e as Map<String, dynamic>))
          .toList(),
    );
  }

  Map<String, dynamic> toJson() {
    return {
      'period': period,
      'start_date': startDate.toIso8601String(),
      'end_date': endDate.toIso8601String(),
      'total_earnings': totalEarnings,
      'base_fares': baseFares,
      'tips': tips,
      'bonuses': bonuses,
      'deductions': deductions,
      'total_rides': totalRides,
      'completed_rides': completedRides,
      'cancelled_rides': cancelledRides,
      'average_fare': averageFare,
      'average_tip': averageTip,
      'online_hours': onlineHours,
      'earnings_per_hour': earningsPerHour,
      'daily_breakdown': dailyBreakdown.map((e) => e.toJson()).toList(),
    };
  }

  @override
  List<Object?> get props => [
        period,
        startDate,
        endDate,
        totalEarnings,
        totalRides,
      ];
}

class DailyEarnings extends Equatable {
  final DateTime date;
  final double earnings;
  final int rides;
  final double tips;
  final double onlineHours;

  const DailyEarnings({
    required this.date,
    required this.earnings,
    required this.rides,
    required this.tips,
    required this.onlineHours,
  });

  factory DailyEarnings.fromJson(Map<String, dynamic> json) {
    return DailyEarnings(
      date: DateTime.parse(json['date'] as String),
      earnings: (json['earnings'] as num).toDouble(),
      rides: json['rides'] as int,
      tips: (json['tips'] as num).toDouble(),
      onlineHours: (json['online_hours'] as num).toDouble(),
    );
  }

  Map<String, dynamic> toJson() {
    return {
      'date': date.toIso8601String(),
      'earnings': earnings,
      'rides': rides,
      'tips': tips,
      'online_hours': onlineHours,
    };
  }

  @override
  List<Object?> get props => [date, earnings, rides, tips, onlineHours];
}