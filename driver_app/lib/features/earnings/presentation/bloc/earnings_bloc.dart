import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:equatable/equatable.dart';

import '../../domain/entities/earnings_report.dart';
import '../../domain/usecases/get_daily_earnings_usecase.dart';
import '../../domain/usecases/get_weekly_earnings_usecase.dart';

// Events
abstract class EarningsEvent extends Equatable {
  const EarningsEvent();

  @override
  List<Object?> get props => [];
}

class LoadDailyEarnings extends EarningsEvent {
  final DateTime date;

  const LoadDailyEarnings({required this.date});

  @override
  List<Object?> get props => [date];
}

class LoadWeeklyEarnings extends EarningsEvent {
  final DateTime startDate;

  const LoadWeeklyEarnings({required this.startDate});

  @override
  List<Object?> get props => [startDate];
}

// States
abstract class EarningsState extends Equatable {
  const EarningsState();

  @override
  List<Object?> get props => [];
}

class EarningsInitial extends EarningsState {
  const EarningsInitial();
}

class EarningsLoading extends EarningsState {
  const EarningsLoading();
}

class EarningsLoaded extends EarningsState {
  final EarningsReport report;

  const EarningsLoaded({required this.report});

  @override
  List<Object?> get props => [report];
}

class EarningsError extends EarningsState {
  final String message;

  const EarningsError({required this.message});

  @override
  List<Object?> get props => [message];
}

// BLoC
class EarningsBloc extends Bloc<EarningsEvent, EarningsState> {
  final GetDailyEarningsUseCase _getDailyEarningsUseCase;
  final GetWeeklyEarningsUseCase _getWeeklyEarningsUseCase;

  EarningsBloc({
    required GetDailyEarningsUseCase getDailyEarningsUseCase,
    required GetWeeklyEarningsUseCase getWeeklyEarningsUseCase,
  })  : _getDailyEarningsUseCase = getDailyEarningsUseCase,
        _getWeeklyEarningsUseCase = getWeeklyEarningsUseCase,
        super(const EarningsInitial()) {
    on<LoadDailyEarnings>(_onLoadDailyEarnings);
    on<LoadWeeklyEarnings>(_onLoadWeeklyEarnings);
  }

  Future<void> _onLoadDailyEarnings(
    LoadDailyEarnings event,
    Emitter<EarningsState> emit,
  ) async {
    emit(const EarningsLoading());

    try {
      final report = await _getDailyEarningsUseCase.execute(event.date);
      emit(EarningsLoaded(report: report));
    } catch (e) {
      emit(EarningsError(message: e.toString()));
    }
  }

  Future<void> _onLoadWeeklyEarnings(
    LoadWeeklyEarnings event,
    Emitter<EarningsState> emit,
  ) async {
    emit(const EarningsLoading());

    try {
      final report = await _getWeeklyEarningsUseCase.execute(event.startDate);
      emit(EarningsLoaded(report: report));
    } catch (e) {
      emit(EarningsError(message: e.toString()));
    }
  }
}