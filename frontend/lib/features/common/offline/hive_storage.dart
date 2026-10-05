import 'dart:convert';
import 'package:hive/hive.dart';

/// Generic Hive storage wrapper for offline data persistence
class HiveStorage {
  static const String _cacheBoxName = 'nidaw_cache';
  static const String _queueBoxName = 'nidaw_queue';
  static const String _settingsBoxName = 'nidaw_settings';

  Box? _cacheBox;
  Box? _queueBox;
  Box? _settingsBox;

  /// Initialize all Hive boxes
  Future<void> initialize() async {
    _cacheBox = await Hive.openBox(_cacheBoxName);
    _queueBox = await Hive.openBox(_queueBoxName);
    _settingsBox = await Hive.openBox(_settingsBoxName);
  }

  // ============================================================================
  // CACHE OPERATIONS
  // ============================================================================

  /// Store data in cache with optional TTL
  Future<void> cache<T>(
    String key,
    T value, {
    Duration? ttl,
  }) async {
    final cacheEntry = {
      'data': _serialize(value),
      'timestamp': DateTime.now().toIso8601String(),
      'ttl': ttl?.inSeconds,
    };
    await _cacheBox?.put(key, cacheEntry);
  }

  /// Retrieve data from cache
  T? getCached<T>(String key) {
    final entry = _cacheBox?.get(key);
    if (entry == null) return null;

    // Check TTL
    final timestamp = DateTime.parse(entry['timestamp'] as String);
    final ttlSeconds = entry['ttl'] as int?;
    if (ttlSeconds != null) {
      final expiry = timestamp.add(Duration(seconds: ttlSeconds));
      if (DateTime.now().isAfter(expiry)) {
        _cacheBox?.delete(key);
        return null;
      }
    }

    return _deserialize<T>(entry['data']);
  }

  /// Check if key exists in cache and is not expired
  bool hasCached(String key) {
    return getCached(key) != null;
  }

  /// Remove item from cache
  Future<void> removeCached(String key) async {
    await _cacheBox?.delete(key);
  }

  /// Clear all cached data
  Future<void> clearCache() async {
    await _cacheBox?.clear();
  }

  /// Get all cache keys
  List<String> getCacheKeys() {
    return _cacheBox?.keys.cast<String>().toList() ?? [];
  }

  // ============================================================================
  // OFFLINE QUEUE OPERATIONS
  // ============================================================================

  /// Add operation to offline queue
  Future<String> enqueueOperation({
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
      'maxRetries': 3,
    };
    await _queueBox?.put(id, operation);
    return id;
  }

  /// Get all pending operations sorted by priority
  List<Map<String, dynamic>> getPendingOperations() {
    final operations = _queueBox?.values.cast<Map>().toList() ?? [];
    operations.sort((a, b) => (b['priority'] as int).compareTo(a['priority'] as int));
    return operations.cast<Map<String, dynamic>>();
  }

  /// Mark operation as completed
  Future<void> completeOperation(String id) async {
    await _queueBox?.delete(id);
  }

  /// Increment retry count for failed operation
  Future<void> retryOperation(String id) async {
    final operation = _queueBox?.get(id) as Map?;
    if (operation == null) return;

    final retryCount = (operation['retryCount'] as int) + 1;
    final maxRetries = operation['maxRetries'] as int;

    if (retryCount >= maxRetries) {
      // Remove from queue after max retries
      await _queueBox?.delete(id);
      return;
    }

    operation['retryCount'] = retryCount;
    operation['lastRetry'] = DateTime.now().toIso8601String();
    await _queueBox?.put(id, operation);
  }

  /// Clear all pending operations
  Future<void> clearQueue() async {
    await _queueBox?.clear();
  }

  /// Get count of pending operations
  int get pendingOperationsCount => _queueBox?.length ?? 0;

  // ============================================================================
  // SETTINGS OPERATIONS
  // ============================================================================

  /// Save a setting
  Future<void> setSetting<T>(String key, T value) async {
    await _settingsBox?.put(key, value);
  }

  /// Get a setting
  T? getSetting<T>(String key, {T? defaultValue}) {
    final value = _settingsBox?.get(key);
    if (value == null) return defaultValue;
    return value as T;
  }

  /// Remove a setting
  Future<void> removeSetting(String key) async {
    await _settingsBox?.delete(key);
  }

  /// Clear all settings
  Future<void> clearSettings() async {
    await _settingsBox?.clear();
  }

  // ============================================================================
  // SERIALIZATION HELPERS
  // ============================================================================

  dynamic _serialize<T>(T value) {
    if (value is Map || value is List) {
      return jsonEncode(value);
    }
    return value;
  }

  T? _deserialize<T>(dynamic value) {
    if (value == null) return null;
    
    if (T == Map<String, dynamic> || T == List<dynamic>) {
      return jsonDecode(value as String) as T;
    }
    
    return value as T;
  }

  // ============================================================================
  // CLEANUP
  // ============================================================================

  /// Close all boxes
  Future<void> close() async {
    await _cacheBox?.close();
    await _queueBox?.close();
    await _settingsBox?.close();
  }

  /// Delete all boxes
  Future<void> deleteAll() async {
    await _cacheBox?.deleteFromDisk();
    await _queueBox?.deleteFromDisk();
    await _settingsBox?.deleteFromDisk();
  }
}