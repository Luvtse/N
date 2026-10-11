import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

/// Client-side view of `config/feature-flags/flags.json`.
///
/// The canonical flag definitions live in the repo's single feature-flag
/// file (Global Rule 6). This service loads that asset at boot and answers
/// "is this surface on for this user?" without ever throwing to the caller:
/// a missing or malformed flag file degrades every flag to OFF, which is the
/// safe default. Production must never inherit an accidental ON from a
/// packaging mistake.
///
/// Kill switch: set `"enabled": false` on any flag in flags.json and ship —
/// [isEnabled] then returns false for every user within one app boot, no
/// code change required. For `ar_navigation` the paired client-side kill
/// switch is `--dart-define=FEATURE_AR_NAVIGATION=false`, which forces the
/// compile-time gate off regardless of what the asset says (see
/// `AppConstants.enableARNavigation` usage in the router).
class FeatureFlagService {
  FeatureFlagService._(this._flags);

  final Map<String, dynamic> _flags;

  /// Loads `assets/config/feature-flags/flags.json` from the bundled app
  /// assets. Call once during bootstrap before runApp. Never throws —
  /// failures resolve to an empty flag set with a debug-mode log line, so
  /// every flag evaluates to disabled.
  static Future<FeatureFlagService> load() async {
    try {
      final raw = await rootBundle.loadString(
        'assets/config/feature-flags/flags.json',
      );
      return FeatureFlagService._parse(raw);
    } catch (e) {
      debugPrint('FeatureFlagService: flag file unavailable ($e); '
          'all flags default to disabled.');
      return FeatureFlagService._(<String, dynamic>{});
    }
  }

  factory FeatureFlagService._parse(String raw) {
    try {
      final decoded = jsonDecode(raw);
      if (decoded is! Map<String, dynamic>) {
        return FeatureFlagService._(<String, dynamic>{});
      }
      final flags = decoded['flags'];
      if (flags is! Map<String, dynamic>) {
        return FeatureFlagService._(<String, dynamic>{});
      }
      // flags.json nests flags by category ("core_services", "experimental",
      // ...). Flatten to a single name -> definition map so callers address
      // flags by name only.
      final flat = <String, dynamic>{};
      for (final category in flags.values) {
        if (category is Map<String, dynamic>) {
          for (final entry in category.entries) {
            if (entry.value is Map<String, dynamic>) {
              flat[entry.key] = entry.value;
            }
          }
        }
      }
      return FeatureFlagService._(flat);
    } catch (e) {
      debugPrint('FeatureFlagService: unparseable flag file ($e); '
          'all flags default to disabled.');
      return FeatureFlagService._(<String, dynamic>{});
    }
  }

  /// Whether [flagName] is on for the current device/user.
  ///
  /// Evaluation order (short-circuits on the first NO):
  /// 1. Flag absent from the shipped file → false.
  /// 2. `"enabled": false` → false (kill switch).
  /// 3. Current build mode not listed in `targeting.environments` → false.
  ///    Release builds match "production"; profile matches "staging";
  ///    debug matches "development".
  /// 4. Rollout percentage gate: deterministic bucket derived from
  ///    [deviceId] (stable per install, so a user does not flicker between
  ///    variants across launches). rolloutPercentage >= 100 skips hashing;
  ///    <= 0 disables. No deviceId available → conservative false for
  ///    partial rollouts, true for full rollouts.
  bool isEnabled(String flagName, {String? deviceId}) {
    final def = _flags[flagName];
    if (def is! Map<String, dynamic>) return false;
    if (def['enabled'] != true) return false;

    final targeting = def['targeting'];
    if (targeting is Map<String, dynamic>) {
      final environments = targeting['environments'];
      if (environments is List && environments.isNotEmpty) {
        if (!environments.contains(_currentEnvironment)) return false;
      }
    }

    final pct = def['rolloutPercentage'];
    if (pct is! num) return false;
    if (pct >= 100) return true;
    if (pct <= 0) return false;
    if (deviceId == null || deviceId.isEmpty) return false;
    return _bucketFor(flagName, deviceId) < pct;
  }

  String get _currentEnvironment {
    if (kReleaseMode) return 'production';
    if (kProfileMode) return 'staging';
    return 'development';
  }

  /// Stable 0..99 bucket. FNV-1a over "flagName:deviceId" — cheap,
  /// dependency-free, and consistent across Dart platforms.
  int _bucketFor(String flagName, String deviceId) {
    const prime = 0x01000193;
    var hash = 0x811c9dc5;
    for (final unit in utf8.encode('$flagName:$deviceId')) {
      hash = ((hash ^ unit) * prime) & 0xFFFFFFFF;
    }
    return hash % 100;
  }

  /// Names of every flag currently defined — used by the debug drawer to
  /// render real evaluation state instead of hardcoded guesses.
  List<String> get definedFlagNames =>
      (_flags.keys.toList(growable: false))..sort();

  /// Raw definition lookup for diagnostics. Returns null when unknown.
  Map<String, dynamic>? definitionOf(String flagName) {
    final def = _flags[flagName];
    return def is Map<String, dynamic> ? Map<String, dynamic>.from(def) : null;
  }

  @visibleForTesting
  factory FeatureFlagService.forTests(Map<String, dynamic> flags) =>
      FeatureFlagService._(Map<String, dynamic>.from(flags));

  /// Exposed for tests asserting bucket stability.
  @visibleForTesting
  int bucketFor(String flagName, String deviceId) =>
      _bucketFor(flagName, deviceId);
}
