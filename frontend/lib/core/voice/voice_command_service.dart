import 'dart:async';
import 'package:speech_to_text/speech_to_text.dart' as stt;
import 'package:flutter_tts/flutter_tts.dart';

class VoiceCommandService {
  static final VoiceCommandService _instance = VoiceCommandService._internal();
  factory VoiceCommandService() => _instance;
  VoiceCommandService._internal();
  
  final stt.SpeechToText _speech = stt.SpeechToText();
  final FlutterTts _tts = FlutterTts();
  
  bool _isListening = false;
  String _lastCommand = '';
  StreamController<VoiceCommand>? _commandController;
  
  Stream<VoiceCommand> get commandStream => _commandController!.stream;
  
  Future<void> initialize() async {
    await _speech.initialize();
    await _tts.setLanguage("en-US");
    _commandController = StreamController.broadcast();
  }
  
  Future<void> startListening() async {
    if (_isListening) return;
    
    _isListening = true;
    
    await _speech.listen(
      onResult: (result) {
        _processCommand(result.recognizedWords);
      },
      listenFor: const Duration(seconds: 10),
      pauseFor: const Duration(seconds: 2),
      partialResults: true,
      localeId: "en_US",
      onSoundLevelChange: (level) {
        // Visual feedback for sound level
      },
    );
  }
  
  Future<void> stopListening() async {
    if (!_isListening) return;
    _isListening = false;
    await _speech.stop();
  }
  
  void _processCommand(String text) {
    if (text.isEmpty) return;
    
    _lastCommand = text;
    final command = _parseCommand(text);
    
    if (command != null) {
      _commandController?.add(command);
      _speakFeedback(command.feedback);
    }
  }
  
  VoiceCommand? _parseCommand(String text) {
    final lower = text.toLowerCase();
    
    // Ride commands
    if (lower.contains('book a ride') || lower.contains('order a ride')) {
      final destination = _extractDestination(lower);
      return VoiceCommand(
        type: CommandType.bookRide,
        parameters: {'destination': destination},
        feedback: 'Booking a ride to $destination',
      );
    }
    
    if (lower.contains('where is my driver') || lower.contains('driver location')) {
      return VoiceCommand(
        type: CommandType.driverLocation,
        parameters: {},
        feedback: 'Your driver is 3 minutes away',
      );
    }
    
    if (lower.contains('cancel ride')) {
      return VoiceCommand(
        type: CommandType.cancelRide,
        parameters: {},
        feedback: 'Cancelling your ride',
      );
    }
    
    // Hotel commands
    if (lower.contains('find a hotel') || lower.contains('book hotel')) {
      final city = _extractCity(lower);
      final dates = _extractDates(lower);
      return VoiceCommand(
        type: CommandType.findHotel,
        parameters: {'city': city, 'dates': dates},
        feedback: 'Searching for hotels in $city',
      );
    }
    
    // Food commands
    if (lower.contains('order food') || lower.contains('I\'m hungry')) {
      final cuisine = _extractCuisine(lower);
      return VoiceCommand(
        type: CommandType.orderFood,
        parameters: {'cuisine': cuisine},
        feedback: 'Finding $cuisine restaurants near you',
      );
    }
    
    // Navigation commands
    if (lower.contains('navigate') || lower.contains('directions')) {
      return VoiceCommand(
        type: CommandType.navigate,
        parameters: {'mode': _extractNavigationMode(lower)},
        feedback: 'Starting navigation',
      );
    }
    
    if (lower.contains('ar mode') || lower.contains('augmented reality')) {
      return VoiceCommand(
        type: CommandType.enableAR,
        parameters: {},
        feedback: 'Opening AR navigation',
      );
    }
    
    // General commands
    if (lower.contains('go home') || lower.contains('home screen')) {
      return VoiceCommand(
        type: CommandType.goHome,
        parameters: {},
        feedback: 'Going to home screen',
      );
    }
    
    if (lower.contains('help')) {
      return VoiceCommand(
        type: CommandType.help,
        parameters: {},
        feedback: 'You can say things like: book a ride, find a hotel, order food, or navigate',
      );
    }
    
    return null;
  }
  
  String _extractDestination(String text) {
    // Simple extraction - in production, use NLP
    final patterns = ['to', 'at', 'in'];
    for (final pattern in patterns) {
      final index = text.indexOf(pattern);
      if (index != -1) {
        return text.substring(index + pattern.length).trim();
      }
    }
    return 'your destination';
  }
  
  String _extractCity(String text) {
    // Extract city name
    final patterns = ['in', 'at', 'to'];
    for (final pattern in patterns) {
      final index = text.indexOf(pattern);
      if (index != -1) {
        return text.substring(index + pattern.length).trim();
      }
    }
    return 'your area';
  }
  
  Map<String, String> _extractDates(String text) {
    // Extract dates - simplified
    return {
      'checkIn': 'tomorrow',
      'checkOut': 'in 3 days',
    };
  }
  
  String _extractCuisine(String text) {
    final cuisines = ['pizza', 'sushi', 'burger', 'indian', 'chinese', 'mexican', 'italian'];
    for (final cuisine in cuisines) {
      if (text.contains(cuisine)) {
        return cuisine;
      }
    }
    return 'your favorite';
  }
  
  String _extractNavigationMode(String text) {
    if (text.contains('walk') || text.contains('walking')) return 'walking';
    if (text.contains('drive') || text.contains('driving')) return 'driving';
    if (text.contains('bike') || text.contains('cycling')) return 'cycling';
    return 'driving';
  }
  
  Future<void> _speakFeedback(String feedback) async {
    await _tts.speak(feedback);
  }
  
  bool get isListening => _isListening;
  String get lastCommand => _lastCommand;
  
  void dispose() {
    _commandController?.close();
    _speech.stop();
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
}

class VoiceCommand {
  final CommandType type;
  final Map<String, dynamic> parameters;
  final String feedback;
  
  VoiceCommand({
    required this.type,
    required this.parameters,
    required this.feedback,
  });
}