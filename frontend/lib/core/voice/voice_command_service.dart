import 'dart:async';

import 'package:speech_to_text/speech_to_text.dart' as stt;
import 'package:flutter_tts/flutter_tts.dart';

/// Voice command surface for §4.2 (Voice Booking GA) and the broader
/// super-app voice rail.
///
/// Contract notes that matter downstream:
/// - [VoiceCommand.confidence] carries the recognizer's confidence for the
///   final transcript (0..1). The request page uses it to decide whether a
///   `bookRide` intent may auto-fill the destination or must fall back to the
///   prefilled autocomplete sheet — see [VoiceCommandService.lowConfidenceThreshold].
/// - No feedback string ever asserts a live fact the service cannot read
///   (the old "Your driver is 3 minutes away" was fabricated). Feedback here
///   only repeats what the rider said or states an honest failure.
class VoiceCommandService {
  static final VoiceCommandService _instance = VoiceCommandService._internal();
  factory VoiceCommandService() => _instance;
  VoiceCommandService._internal();

  final stt.SpeechToText _speech = stt.SpeechToText();
  final FlutterTts _tts = FlutterTts();

  bool _isListening = false;
  bool _initialized = false;
  String _lastCommand = '';
  /// Confidence of the most recent finalized utterance, 0..1. Null until at
  /// least one listen() has produced a result.
  double? _lastConfidence;
  StreamController<VoiceCommand>? _commandController;

  Stream<VoiceCommand> get commandStream =>
      _commandController?.stream ?? const Stream<VoiceCommand>.empty();

  /// Below this recognizer confidence a booking transcript is treated as
  /// ambiguous and the UI must route it through manual confirmation
  /// (prefilled autocomplete sheet) instead of acting on it silently.
  static const double lowConfidenceThreshold = 0.75;

  /// Completes with true when the mic is usable (permission granted and the
  /// platform recognizer initialized). Callers must surface a false result
  /// honestly ("Voice input unavailable") rather than starting a dead session.
  Future<bool> initialize() async {
    if (_initialized) return true;
    // speech_to_text.initialize() itself requests the microphone permission
    // on Android/iOS and returns false when the user denies it.
    final ok = await _speech.initialize(
      onError: (e) {
        // Surface recognition errors as an error-type command so listeners
        // can show an honest snackbar instead of silently going quiet.
        _commandController?.add(VoiceCommand(
          type: CommandType.error,
          parameters: {'message': e.msg},
          feedback: 'Voice input error: ${e.msg}',
          confidence: 0,
        ));
      },
      onStatus: (status) {
        if (status == 'done' || status == 'notYet') {
          _isListening = false;
        }
      },
    );
    _initialized = ok;
    if (ok) {
      try {
        await _tts.setLanguage('en-US');
      } catch (_) {
        // TTS language selection failing must not disable listening; spoken
        // feedback simply falls back to the platform default voice.
      }
    }
    _commandController ??= StreamController.broadcast();
    return ok;
  }

  /// Starts a listening session. Returns false when the recognizer is not
  /// available (uninitialized or permission denied) so the caller can show a
  /// real unavailable state.
  Future<bool> startListening() async {
    if (_isListening) return true;
    if (!_initialized) {
      final ok = await initialize();
      if (!ok) return false;
    }
    _isListening = true;
    if (_commandController == null || _commandController!.isClosed) {
      _commandController = StreamController.broadcast();
    }

    await _speech.listen(
      // Partial results are emitted for live display only; commands fire on
      // the FINAL result (lastSpeech/done) so half-heard transcripts never
      // book rides.
      onResult: (result) {
        final isFinal = result.lastSpeech || result.done;
        _lastConfidence = result.confidence;
        if (isFinal) {
          _processCommand(result.recognizedWords, result.confidence);
        }
      },
      listenFor: const Duration(seconds: 10),
      pauseFor: const Duration(seconds: 2),
      partialResults: true,
      localeId: 'en_US',
      onSoundLevelChange: (level) {
        // Visual feedback hook for the listening sheet (sound amplitude).
      },
    );
    return true;
  }

  Future<void> stopListening() async {
    if (!_isListening) return;
    _isListening = false;
    await _speech.stop();
  }

  void _processCommand(String text, double confidence) {
    if (text.trim().isEmpty) return;

    _lastCommand = text;
    final command = _parseCommand(text, confidence);

    if (command != null) {
      _commandController?.add(command);
      _speakFeedback(command.feedback);
    } else {
      // Honest no-match feedback: repeat what we heard and say we did not
      // understand — never invent an action.
      _commandController?.add(VoiceCommand(
        type: CommandType.noMatch,
        parameters: {'transcript': text},
        feedback: 'I heard "$text" but didn\'t catch a command. Say "help" '
            'for examples.',
        confidence: confidence,
      ));
    }
  }

  VoiceCommand? _parseCommand(String text, double confidence) {
    final lower = text.toLowerCase();

    // Ride commands
    if (lower.contains('book a ride') ||
        lower.contains('order a ride') ||
        lower.contains('get me a ride') ||
        lower.contains('ride to')) {
      final destination = _extractDestination(text);
      return VoiceCommand(
        type: CommandType.bookRide,
        parameters: {
          'destination': destination,
          // Raw transcript kept for the low-confidence fallback path so the
          // autocomplete sheet can prefill exactly what the rider said.
          'transcript': text,
        },
        // Feedback deliberately avoids promising a booking outcome; the
        // request page decides what happens next based on confidence.
        feedback: destination.isEmpty
            ? 'Where to? Say the destination after "ride to".'
            : 'Got it: $destination',
        confidence: confidence,
      );
    }

    if (lower.contains('where is my driver') ||
        lower.contains('driver location') ||
        lower.contains('eta')) {
      // The service has no access to live ride state, so it must not claim an
      // ETA. It hands off to the tracking surface; the page renders the real
      // value from bloc state.
      return VoiceCommand(
        type: CommandType.driverLocation,
        parameters: {},
        feedback: 'Opening your trip so you can see the live arrival time.',
        confidence: confidence,
      );
    }

    if (lower.contains('cancel ride')) {
      return VoiceCommand(
        type: CommandType.cancelRide,
        parameters: {},
        feedback: 'Opening cancellation — review the fee before confirming.',
        confidence: confidence,
      );
    }

    // Hotel commands
    if (lower.contains('find a hotel') || lower.contains('book hotel')) {
      final city = _extractCity(lower);
      return VoiceCommand(
        type: CommandType.findHotel,
        parameters: {'city': city},
        feedback: city.isEmpty
            ? 'Which city should I search hotels in?'
            : 'Searching for hotels in $city',
        confidence: confidence,
      );
    }

    // Food commands
    if (lower.contains('order food') || lower.contains('i\'m hungry')) {
      final cuisine = _extractCuisine(lower);
      return VoiceCommand(
        type: CommandType.orderFood,
        parameters: {'cuisine': cuisine},
        feedback: 'Finding food options near you',
        confidence: confidence,
      );
    }

    // Navigation commands
    if (lower.contains('navigate') || lower.contains('directions')) {
      return VoiceCommand(
        type: CommandType.navigate,
        parameters: {'mode': _extractNavigationMode(lower)},
        feedback: 'Starting navigation',
        confidence: confidence,
      );
    }

    if (lower.contains('ar mode') || lower.contains('augmented reality')) {
      return VoiceCommand(
        type: CommandType.enableAR,
        parameters: {},
        feedback: 'Opening AR navigation',
        confidence: confidence,
      );
    }

    // General commands
    if (lower.contains('go home') || lower.contains('home screen')) {
      return VoiceCommand(
        type: CommandType.goHome,
        parameters: {},
        feedback: 'Going to home screen',
        confidence: confidence,
      );
    }

    if (lower.contains('help')) {
      return VoiceCommand(
        type: CommandType.help,
        parameters: {},
        feedback: 'You can say: book a ride to a place, where is my driver, '
            'cancel ride, find a hotel, or order food.',
        confidence: confidence,
      );
    }

    return null;
  }

  /// Extracts the destination from the ORIGINAL-cased text so place names
  /// survive ("ride to Bole Airport" -> "Bole Airport"). Returns '' when no
  /// destination phrase matched — callers treat empty as "ask again", never
  /// as a literal "your destination" placeholder.
  String _extractDestination(String original) {
    final match = RegExp(
      r'\b(?:book|order|get|request)\s+(?:me\s+)?(?:a\s+)?ride\s+to\s+(.+)$'
      r'|\bride\s+to\s+(.+)$'
      r'|\bto\s+(.+)$',
      caseSensitive: false,
    ).firstMatch(original.trim());
    if (match == null) return '';
    final captured =
        (match.group(1) ?? match.group(2) ?? match.group(3) ?? '').trim();
    // Strip trailing filler words that speech recognizers commonly append.
    return captured
        .replaceAll(RegExp(r'\s+(please|now|thanks|thank you)[.!]*$'), '')
        .trim();
  }

  String _extractCity(String text) {
    final match =
        RegExp(r'(?:in|at|to)\s+([a-z][a-z\s\-]{2,})$', caseSensitive: false)
            .firstMatch(text.trim());
    return match?.group(1)?.trim() ?? '';
  }

  String _extractCuisine(String text) {
    const cuisines = [
      'pizza', 'sushi', 'burger', 'indian', 'chinese', 'mexican', 'italian',
      'ethiopian', 'injera', 'tibs',
    ];
    for (final cuisine in cuisines) {
      if (text.contains(cuisine)) {
        return cuisine;
      }
    }
    return '';
  }

  String _extractNavigationMode(String text) {
    if (text.contains('walk') || text.contains('walking')) return 'walking';
    if (text.contains('bike') || text.contains('cycling')) return 'cycling';
    return 'driving';
  }

  Future<void> _speakFeedback(String feedback) async {
    try {
      await _tts.speak(feedback);
    } catch (_) {
      // Spoken feedback is best-effort; the same string is rendered visually
      // by the listening sheet, so nothing is lost if TTS fails.
    }
  }

  bool get isListening => _isListening;
  String get lastCommand => _lastCommand;
  double? get lastConfidence => _lastConfidence;

  void dispose() {
    _commandController?.close();
    _commandController = null;
    _speech.cancel();
    _isListening = false;
    _initialized = false;
  }
}

enum CommandType {
  bookRide,
  driverLocation,
  cancelRide,
  findHotel,
  orderFood,
  navigate,
  enableAR,
  goHome,
  help,
  /// Recognizer/permission failure surfaced to the UI.
  error,
  /// Utterance captured but no intent matched — carries the raw transcript.
  noMatch,
}

class VoiceCommand {
  final CommandType type;
  final Map<String, dynamic> parameters;
  final String feedback;

  /// Recognizer confidence for the utterance that produced this command,
  /// 0..1. Commands synthesized by the service itself (errors) use 0.
  final double confidence;

  VoiceCommand({
    required this.type,
    required this.parameters,
    required this.feedback,
    required this.confidence,
  });

  /// True when the transcript is trustworthy enough to act on without an
  /// extra confirmation step.
  bool get isHighConfidence =>
      confidence >= VoiceCommandService.lowConfidenceThreshold;
}
