import 'dart:async';
import 'dart:collection';

/// Token-bucket rate limiter enforcing Nominatim's usage policy of at most
/// one request per second (§1.3 step 4).
///
/// The UI layer additionally debounces autocomplete queries by 300 ms; this
/// service-layer bucket is the second line of defense so rapid programmatic
/// calls (or multiple widgets sharing the service) can never exceed 1 rps
/// against the free provider.
class GeocodingRateLimiter {
  /// Maximum sustained requests per second.
  final double requestsPerSecond;

  final Queue<Completer<void>> _waiters = Queue<Completer<void>>();

  /// Virtual-clock reservation: the next permit may not be issued before this
  /// instant, even when many callers arrive at once (each acquire reserves its
  /// own slot, so N simultaneous waiters spread out over N intervals).
  DateTime? _nextPermitAt;
  Timer? _timer;

  GeocodingRateLimiter({this.requestsPerSecond = 1.0});

  Duration get _minInterval =>
      Duration(microseconds: (1e6 / requestsPerSecond).round());

  /// Completes once the caller may issue a request. FIFO fairness: waiters are
  /// served in arrival order so no query starves.
  Future<void> acquire() {
    final completer = Completer<void>();
    _waiters.add(completer);
    _schedule();
    return completer.future;
  }

  void _schedule() {
    if (_waiters.isEmpty || _timer != null) return;
    final now = DateTime.now();
    final base = _nextPermitAt ?? now;
    final due = base.isBefore(now) ? now : base;
    // Reserve this slot up-front so simultaneous acquirers spread out.
    _nextPermitAt = due.add(_minInterval);
    final delay = due.difference(now);
    if (delay <= Duration.zero) {
      _waiters.removeFirst().complete();
      if (_waiters.isNotEmpty) _schedule();
    } else {
      _timer = Timer(delay, () {
        _timer = null;
        if (_waiters.isNotEmpty) {
          _waiters.removeFirst().complete();
          _schedule();
        }
      });
    }
  }

  /// Test hook / shutdown: cancel the pending timer and fail every waiter so
  /// callers don't hang.
  void dispose() {
    _timer?.cancel();
    _timer = null;
    while (_waiters.isNotEmpty) {
      _waiters.removeFirst().completeError(
          StateError('GeocodingRateLimiter disposed while waiting'));
    }
  }
}
