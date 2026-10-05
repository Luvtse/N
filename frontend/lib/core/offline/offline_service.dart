import 'dart:async';
import 'dart:convert';

import 'package:connectivity_plus/connectivity_plus.dart';
import 'package:hive/hive.dart';

/// Offline service for handling operations when network is unavailable
class OfflineService {
  static const String _pendingOperationsBox = 'pending_operations';
  static const String _cachedDataBox = 'cached_data';
  
  late Box _pendingOpsBox;
  late Box _cachedDataBox;
  
  final Connectivity _connectivity = Connectivity();
  StreamSubscription? _connectivitySubscription;
  
  bool _isOnline = true;
  final StreamController<bool> _connectivityController = StreamController.broadcast();

  bool get isOnline => _isOnline;
  Stream<bool> get connectivityStream => _connectivityController.stream;

  Future<void> initialize() async {
    _pendingOpsBox = await Hive.openBox(_pendingOperationsBox);
    _cachedDataBox = await Hive.openBox(_cachedDataBox);
    
    // Check initial connectivity
    final result = await _connectivity.checkConnectivity();
    _isOnline = result != ConnectivityResult.none;
    
    // Listen for connectivity changes
    _connectivitySubscription = _connectivity.onConnectivityChanged.listen((result) {
      final wasOffline = !_isOnline;
      _isOnline = result != ConnectivityResult.none;
      
      _connectivityController.add(_isOnline);
      
      if (wasOffline && _isOnline) {
        syncPendingOperations();
      }
    });
  }

  // ==========================================================================
  // PENDING OPERATIONS
  // ==========================================================================

  /// Queue an operation for later execution
  Future<String> queueOperation({
    required String endpoint,
    required String method,
    required Map<String, dynamic> data,
    int priority = 0,
  }) async {
    final id = DateTime.now().millisecondsSinceEpoch.toString();
    
    final operation = {
      'id': id,
      'endpoint': endpoint,
      'method': method,
      'data': data,
      'priority': priority,
      'timestamp': DateTime.now().toIso8601String(),
      'retryCount': 0,
    };
    
    await _pendingOpsBox.put(id, operation);
    return id;
  }

  /// Sync all pending operations when back online
  Future<void> syncPendingOperations() async {
    if (!_isOnline) return;
    
    final operations = _pendingOpsBox.values.toList()
      ..sort((a, b) => (b['priority'] as int).compareTo(a['priority'] as int));
    
    for (final op in operations) {
      try {
        await _executeOperation(op);
        await _pendingOpsBox.delete(op['id']);
      } catch (e) {
        int retryCount = op['retryCount'] ?? 0;
        if (retryCount < 3) {
          op['retryCount'] = retryCount + 1;
          await _pendingOpsBox.put(op['id'], op);
        } else {
          await _pendingOpsBox.delete(op['id']);
          // TODO: Notify user of failed operation
        }
      }
    }
  }

  Future<void> _executeOperation(Map<String, dynamic> operation) async {
    // This would integrate with your ApiClient
    // For now, just a placeholder
    final endpoint = operation['endpoint'] as String;
    final method = operation['method'] as String;
    final data = operation['data'] as Map<String, dynamic>;
    
    // TODO: Call actual API
    print('Executing offline operation: $method $endpoint');
  }

  int get pendingOperationCount => _pendingOpsBox.length;

  // ==========================================================================
  // CACHED DATA
  // ==========================================================================

  /// Cache data for offline access
  Future<void> cacheData(String key, dynamic data, {Duration? ttl}) async {
    final cacheEntry = {
      'data': data,
      'cachedAt': DateTime.now().toIso8601String(),
      'expiresAt': ttl != null 
          ? DateTime.now().add(ttl).toIso8601String()
          : null,
    };
    
    await _cachedDataBox.put(key, cacheEntry);
  }

  /// Get cached data
  dynamic getCachedData(String key) {
    final entry = _cachedDataBox.get(key);
    if (entry == null) return null;
    
    // Check expiration
    if (entry['expiresAt'] != null) {
      final expiresAt = DateTime.parse(entry['expiresAt']);
      if (DateTime.now().isAfter(expiresAt)) {
        _cachedDataBox.delete(key);
        return null;
      }
    }
    
    return entry['data'];
  }

  /// Check if data is cached
  bool hasCachedData(String key) {
    return getCachedData(key) != null;
  }

  /// Clear all cached data
  Future<void> clearCache() async {
    await _cachedDataBox.clear();
  }

  // ==========================================================================
  // CLEANUP
  // ==========================================================================

  void dispose() {
    _connectivitySubscription?.cancel();
    _connectivityController.close();
  }
}