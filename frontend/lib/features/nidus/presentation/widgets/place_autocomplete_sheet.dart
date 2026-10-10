import 'dart:async';

import 'package:flutter/material.dart';
import 'package:latlong2/latlong.dart';

import '../../../../core/geocoding/geocoding_models.dart';
import '../../../../core/geocoding/geocoding_service.dart';
import '../../../../core/theme/app_colors.dart';

/// Result contract returned by [PlaceAutocompleteSheet.show]. Exactly one of
/// the fields is meaningful for a given [PlaceAutocompleteOutcomeKind].
enum PlaceAutocompleteOutcomeKind { suggestion, currentLocation, setPin, none }

class PlaceAutocompleteOutcome {
  final PlaceAutocompleteOutcomeKind kind;
  final PlaceSuggestion? suggestion;

  const PlaceAutocompleteOutcome._(this.kind, [this.suggestion]);

  const PlaceAutocompleteOutcome.none()
      : this._(PlaceAutocompleteOutcomeKind.none);
  const PlaceAutocompleteOutcome.currentLocation()
      : this._(PlaceAutocompleteOutcomeKind.currentLocation);
  const PlaceAutocompleteOutcome.setPin()
      : this._(PlaceAutocompleteOutcomeKind.setPin);
  PlaceAutocompleteOutcome.picked(PlaceSuggestion s)
      : this._(PlaceAutocompleteOutcomeKind.suggestion, s);
}

/// Bottom-sheet place search (§1.3 step 5).
///
/// Behavior locked by the plan:
/// - text field debounced 300 ms (the service layer additionally rate-limits
///   Nominatim to 1 rps — defense in depth);
/// - each row shows primary name, secondary city/country line, and distance
///   from [bias] when provided;
/// - pinned "Use current location" row at top;
/// - "Set pin on map" row that closes the sheet and enters pin-drag mode;
/// - empty state: "No matches — try a landmark or street name.";
/// - error state: "Search unavailable — showing cached results" when the
///   provider chain degrades to cache, and an honest retry-only state when
///   even the cache has nothing. Never fabricated rows.
class PlaceAutocompleteSheet extends StatefulWidget {
  final GeocodingService geocoding;
  final String title;
  final LatLng? bias;

  /// Called when the rider taps the pinned "Use current location" row. The
  /// sheet pops with [PlaceAutocompleteOutcome.currentLocation]; the caller
  /// owns the GPS read so permission handling stays in one place.
  final Future<void> Function()? ignoredCurrentLocationHandler;

  const PlaceAutocompleteSheet({
    super.key,
    required this.geocoding,
    required this.title,
    this.bias,
    @Deprecated('The sheet pops with an outcome; callers act on it.')
        this.ignoredCurrentLocationHandler,
  });

  /// Static entry point used by location_input / request page.
  static Future<PlaceAutocompleteOutcome> show(
    BuildContext context, {
    required GeocodingService geocoding,
    required String title,
    LatLng? bias,
  }) async {
    final outcome = await showModalBottomSheet<PlaceAutocompleteOutcome>(
      context: context,
      isScrollControlled: true,
      backgroundColor: Colors.white,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
      ),
      builder: (_) => PlaceAutocompleteSheet(
        geocoding: geocoding,
        title: title,
        bias: bias,
      ),
    );
    return outcome ?? const PlaceAutocompleteOutcome.none();
  }

  @override
  State<PlaceAutocompleteSheet> createState() => _PlaceAutocompleteSheetState();
}

class _PlaceAutocompleteSheetState extends State<PlaceAutocompleteSheet> {
  final TextEditingController _controller = TextEditingController();
  final FocusNode _focusNode = FocusNode();
  Timer? _debounce;

  List<PlaceSuggestion> _results = const [];
  bool _loading = false;
  bool _degradedToCache = false;
  bool _unavailable = false;
  String _query = '';

  static const Duration _debounceDuration = Duration(milliseconds: 300);

  @override
  void dispose() {
    _debounce?.cancel();
    _controller.dispose();
    _focusNode.dispose();
    super.dispose();
  }

  void _onQueryChanged(String value) {
    _debounce?.cancel();
    _query = value;
    if (value.trim().isEmpty) {
      setState(() {
        _results = const [];
        _loading = false;
        _degradedToCache = false;
        _unavailable = false;
      });
      return;
    }
    setState(() => _loading = true);
    _debounce = Timer(_debounceDuration, () => _search(value));
  }

  Future<void> _search(String value) async {
    try {
      final results =
          await widget.geocoding.autocomplete(value, bias: widget.bias);
      if (!mounted || value != _query) return;
      final degraded =
          results.isNotEmpty && results.every((s) => s.fromCache);
      setState(() {
        _results = results;
        _loading = false;
        _degradedToCache = degraded;
        _unavailable = false;
      });
    } on GeocodingUnavailableException {
      if (!mounted || value != _query) return;
      setState(() {
        _loading = false;
        _unavailable = true;
        _results = const [];
      });
    } on Object {
      if (!mounted || value != _query) return;
      setState(() {
        _loading = false;
        _unavailable = true;
        _results = const [];
      });
    }
  }

  void _popWith(PlaceAutocompleteOutcome outcome) =>
      Navigator.of(context).pop(outcome);

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: EdgeInsets.only(
        left: 24,
        right: 24,
        top: 16,
        bottom: MediaQuery.of(context).viewInsets.bottom + 16,
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(
            widget.title,
            style: const TextStyle(
              fontSize: 16,
              fontWeight: FontWeight.w700,
              color: AppColors.textPrimary,
            ),
          ),
          const SizedBox(height: 12),
          TextField(
            controller: _controller,
            focusNode: _focusNode,
            autofocus: true,
            textInputAction: TextInputAction.search,
            onChanged: _onQueryChanged,
            decoration: InputDecoration(
              hintText: 'Search a place or landmark',
              prefixIcon: const Icon(Icons.search, color: AppColors.primary),
              suffixIcon: _loading
                  ? const Padding(
                      padding: EdgeInsets.all(12),
                      child: SizedBox(
                        width: 18,
                        height: 18,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      ),
                    )
                  : null,
              border: OutlineInputBorder(
                borderRadius: BorderRadius.circular(12),
                borderSide: const BorderSide(color: AppColors.inputBorder),
              ),
              enabledBorder: OutlineInputBorder(
                borderRadius: BorderRadius.circular(12),
                borderSide: const BorderSide(color: AppColors.inputBorder),
              ),
              contentPadding:
                  const EdgeInsets.symmetric(horizontal: 12, vertical: 12),
            ),
          ),
          const SizedBox(height: 8),
          Flexible(
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxHeight: 340),
              child: ListView(
                shrinkWrap: true,
                children: [
                  // Pinned rows — always available regardless of query.
                  _PinnedRow(
                    icon: Icons.my_location,
                    label: 'Use current location',
                    onTap: () =>
                        _popWith(const PlaceAutocompleteOutcome.currentLocation()),
                  ),
                  _PinnedRow(
                    icon: Icons.place_outlined,
                    label: 'Set pin on map',
                    onTap: () => _popWith(const PlaceAutocompleteOutcome.setPin()),
                  ),
                  if (_query.trim().isNotEmpty) const Divider(height: 16),
                  ..._buildBody(),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }

  List<Widget> _buildBody() {
    if (_loading && _results.isEmpty) {
      return [
        const Padding(
          padding: EdgeInsets.symmetric(vertical: 24),
          child: Center(child: CircularProgressIndicator()),
        ),
      ];
    }

    if (_unavailable) {
      return [
        _StateRow(
          icon: Icons.wifi_off_rounded,
          message: 'Search unavailable — showing cached results',
          subMessage: _results.isEmpty
              ? 'No cached results for this query either. Check your '
                  'connection and retry.'
              : null,
          actionLabel: 'Retry',
          onAction: () => _search(_query),
        ),
      ];
    }

    if (_degradedToCache && _results.isNotEmpty) {
      return [
        const _StateRow(
          icon: Icons.cloud_off_rounded,
          message: 'Search unavailable — showing cached results',
        ),
        ..._results.map(_resultTile),
      ];
    }

    if (_results.isEmpty) {
      if (_query.trim().isEmpty) return const [];
      return const [
        _StateRow(
          icon: Icons.search_off,
          message: 'No matches — try a landmark or street name.',
        ),
      ];
    }

    return _results.map(_resultTile).toList();
  }

  Widget _resultTile(PlaceSuggestion s) {
    final distance = s.distanceMeters;
    return ListTile(
      contentPadding: EdgeInsets.zero,
      leading: Icon(
        s.fromCache ? Icons.history : Icons.place,
        color: AppColors.textSecondary,
      ),
      title: Text(
        s.name,
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
        style: const TextStyle(
          fontWeight: FontWeight.w600,
          color: AppColors.textPrimary,
        ),
      ),
      subtitle: Text(
        s.detail,
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
        style: const TextStyle(fontSize: 12, color: AppColors.textTertiary),
      ),
      trailing: distance == null
          ? null
          : Text(
              _formatDistance(distance),
              style:
                  const TextStyle(fontSize: 12, color: AppColors.textTertiary),
            ),
      onTap: () => _popWith(PlaceAutocompleteOutcome.picked(s)),
    );
  }

  static String _formatDistance(double meters) {
    if (meters < 1000) return '${meters.round()} m';
    return '${(meters / 1000).toStringAsFixed(1)} km';
  }
}

class _PinnedRow extends StatelessWidget {
  final IconData icon;
  final String label;
  final VoidCallback onTap;

  const _PinnedRow({
    required this.icon,
    required this.label,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    return ListTile(
      contentPadding: EdgeInsets.zero,
      leading: Icon(icon, color: AppColors.primary),
      title: Text(
        label,
        style: const TextStyle(
          fontWeight: FontWeight.w600,
          color: AppColors.textPrimary,
        ),
      ),
      onTap: onTap,
    );
  }
}

class _StateRow extends StatelessWidget {
  final IconData icon;
  final String message;
  final String? subMessage;
  final String? actionLabel;
  final VoidCallback? onAction;

  const _StateRow({
    required this.icon,
    required this.message,
    this.subMessage,
    this.actionLabel,
    this.onAction,
  });

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 20, horizontal: 4),
      child: Row(
        children: [
          Icon(icon, color: AppColors.textTertiary, size: 22),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  message,
                  style: const TextStyle(
                    color: AppColors.textSecondary,
                    fontSize: 14,
                  ),
                ),
                if (subMessage != null) ...[
                  const SizedBox(height: 4),
                  Text(
                    subMessage!,
                    style: const TextStyle(
                      color: AppColors.textTertiary,
                      fontSize: 12,
                    ),
                  ),
                ],
              ],
            ),
          ),
          if (actionLabel != null && onAction != null)
            TextButton(onPressed: onAction, child: Text(actionLabel!)),
        ],
      ),
    );
  }
}
