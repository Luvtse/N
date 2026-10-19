import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:go_router/go_router.dart';
import 'package:latlong2/latlong.dart';

import 'package:get_it/get_it.dart';

import '../../../../core/config/feature_flag_service.dart';
import '../../../../core/di/injection.dart';
import '../../../../core/geocoding/geocoding_models.dart';
import '../../../../core/geocoding/geocoding_service.dart';
import '../../../../core/location/device_location_service.dart';
import '../../../../core/map/live_map_widget.dart';
import '../../../../core/notifications/notification_service.dart';
import '../../../../core/theme/app_colors.dart';
import '../../../../core/voice/voice_command_service.dart';
import '../../data/repositories/ride_repository.dart' show RideStop, maxRideStops;
import '../bloc/ride_bloc.dart';
import '../widgets/location_input.dart';
import '../widgets/location_permission_gate.dart';
import '../widgets/place_autocomplete_sheet.dart';
import '../widgets/payment_method_selector.dart';
import '../widgets/promo_code_sheet.dart';
import '../widgets/ride_type_selector.dart';

/// Request surface, rebuilt on the Foundation substrate:
/// - map region is [LiveMapWidget] (OSM tiles via the fallback chain), not the
///   deleted placeholder stub or the duplicate home page;
/// - pickup comes from a real GPS fix ([DeviceLocationService]) or a
///   long-press-drag of the map pin — never hardcoded demo coordinates;
/// - the map + pickup field sit inside [LocationPermissionGate] so a denied
///   permission shows the gate instead of blanking the screen, while the
///   destination input stays usable.
class RideRequestPage extends StatefulWidget {
  const RideRequestPage({super.key});

  @override
  State<RideRequestPage> createState() => _RideRequestPageState();
}

class _RideRequestPageState extends State<RideRequestPage>
    with WidgetsBindingObserver {
  static const double _defaultZoom = 15.0;

  /// Fallback camera center when GPS is unavailable. This is deliberately NOT
  /// a fabricated pickup location: no pin drops and both fields stay in their
  /// honest "unavailable" state until a real fix arrives. It only positions
  /// the camera so the map is not stuck at 0,0 in the ocean.
  static final LatLng _cameraFallback = LatLng(9.03, 38.74);

  final DeviceLocationService _locationService = const DeviceLocationService();
  final GeocodingService _geocoding = getIt<GeocodingService>();
  final GlobalKey<LiveMapWidgetState> _mapKey =
      GlobalKey<LiveMapWidgetState>();

  Timer? _reverseDebounce;
  int _reverseSeq = 0;

  LocationFieldValue _pickup = const LocationFieldValue.empty();
  LocationFieldValue _dropoff = const LocationFieldValue.empty();

  bool _pickupLocating = false;
  bool _gpsUnavailableBanner = false;
  String _selectedRideType = 'standard';
  String? _selectedPaymentMethodId;

  /// §3.1: rail type of the selected registry method ('wallet' | 'card' |
  /// 'cash'). Null when nothing is selected — the server then resolves the
  /// rider's default instead of the client inventing one.
  String? _selectedPaymentMethodType;

  /// §3.2: code validated by POST /promotions/redeem + its server-projected
  /// discount. The projection is display-only; the authoritative number is
  /// recomputed by the backend at booking time.
  String? _promoCode;
  double? _projectedDiscount;

  /// §3.6: scheduled pickup time (local); null = on-demand.
  DateTime? _scheduledAt;

  /// §3.5: intermediate waypoints in visit order, capped at [maxRideStops]
  /// (the same constant the backend enforces via entities.MaxRideStops and
  /// migration 012's CHECK constraint). Empty list = direct trip; the bloc →
  /// use case → repository chain then omits the `stops` key entirely so the
  /// wire request is byte-identical to a pre-stopovers booking.
  final List<RideStop> _stops = [];

  /// Client mirror of the backend ride_stopovers flag
  /// (FEATURE_RIDE_STOP_OVERS). When off the whole stopover affordance is
  /// hidden — no dead button, per Global Rule 4/6.
  bool get _stopoversEnabled =>
      getIt<FeatureFlagService>().isEnabled('ride_stopovers');

  /// §4.2: client mirror of the voice_booking flag. When off, the mic FAB is
  /// not built at all — no dead button (Global Rule 4/6).
  bool get _voiceBookingEnabled =>
      getIt<FeatureFlagService>().isEnabled('voice_booking');

  /// §4.2: live command subscription while the listening sheet is open.
  StreamSubscription<VoiceCommand>? _voiceSub;

  LatLng get _cameraCenter => _pickup.hasCoordinates
      ? LatLng(_pickup.lat!, _pickup.lng!)
      : _cameraFallback;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    // Paint an instant pin from the last-known position before the first
    // fresh fix arrives (§1.2 requirement).
    _seedFromLastKnown();
    _acquirePickup();
  }

  @override
  void dispose() {
    _reverseDebounce?.cancel();
    // §4.2: never leak the voice stream or leave the mic hot after the page
    // is gone (e.g. navigation to tracking right after a booking command).
    _voiceSub?.cancel();
    VoiceCommandService().stopListening();
    WidgetsBinding.instance.removeObserver(this);
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    // Returning from OS settings / re-enabling GPS: refresh the pin.
    if (state == AppLifecycleState.resumed && !_pickup.hasCoordinates) {
      _acquirePickup();
    }
  }

  Future<void> _seedFromLastKnown() async {
    final result = await _locationService.getLastKnownPosition();
    if (!mounted) return;
    if (result is LocationGranted) {
      setState(() {
        _pickup = LocationFieldValue(
          display: 'Current location (approximate)',
          lat: result.latitude,
          lng: result.longitude,
        );
      });
      _fitCameraToPoints();
    }
  }

  /// Real GPS read for the pickup pin. On denial the surrounding
  /// [LocationPermissionGate] owns the UI; on service-disabled/timeout we show
  /// the honest "unavailable" banner and keep manual pin-drag available.
  Future<void> _acquirePickup() async {
    setState(() => _pickupLocating = true);
    final result = await _locationService.getCurrentPosition();
    if (!mounted) return;
    setState(() {
      _pickupLocating = false;
      if (result is LocationGranted) {
        _pickup = LocationFieldValue(
          display: 'Current location',
          lat: result.latitude,
          lng: result.longitude,
        );
        _gpsUnavailableBanner = false;
      } else if (result is LocationServiceDisabled || result is LocationTimeout) {
        _gpsUnavailableBanner = true;
      }
      // Permission states are rendered by the gate itself.
    });
    if (_pickup.hasCoordinates) _fitCameraToPoints();
  }

  /// Pin drag updates coordinates immediately; the human-readable label comes
  /// from reverse geocoding after a 500 ms debounce (§1.3 step 6). While the
  /// label resolves, the field shows its spinner state — never a stale or
  /// fabricated address.
  void _onPickupPinDragged(LatLng point) {
    setState(() {
      _pickup = LocationFieldValue(
        display: 'Pinned location',
        lat: point.latitude,
        lng: point.longitude,
        resolvingLabel: true,
      );
      _gpsUnavailableBanner = false;
    });
    _debouncedReverseLabel(point);
  }

  void _debouncedReverseLabel(LatLng point) {
    _reverseDebounce?.cancel();
    final seq = ++_reverseSeq;
    _reverseDebounce = Timer(const Duration(milliseconds: 500), () async {
      try {
        final result = await _geocoding.reverse(point);
        if (!mounted || seq != _reverseSeq) return;
        setState(() {
          _pickup = _pickup.copyWith(
            display: result.displayAddress,
            resolvingLabel: false,
          );
        });
      } on GeocodingUnavailableException {
        if (!mounted || seq != _reverseSeq) return;
        // Honest fallback: keep the coordinate-backed "Pinned location"
        // label. No invented address string.
        setState(() {
          _pickup = _pickup.copyWith(resolvingLabel: false);
        });
      } on Object {
        if (!mounted || seq != _reverseSeq) return;
        setState(() {
          _pickup = _pickup.copyWith(resolvingLabel: false);
        });
      }
    });
  }

  void _fitCameraToPoints() {
    // §3.5: the camera auto-fit spans every waypoint in visit order —
    // pickup, stops…, dropoff — not just the two endpoints, so a mid-route
    // stop never sits off-screen (plan §3.5 "map auto-fits to include all
    // points").
    final points = <LatLng>[
      if (_pickup.hasCoordinates) LatLng(_pickup.lat!, _pickup.lng!),
      for (final s in _stops) LatLng(s.lat, s.lng),
      if (_dropoff.hasCoordinates) LatLng(_dropoff.lat!, _dropoff.lng!),
    ];
    final map = _mapKey.currentState;
    if (map == null) return;
    if (points.length >= 2) {
      map.fitBounds(points, paddingDp: 80);
    } else if (points.length == 1) {
      map.moveTo(points.first);
    }
  }

  bool get _canRequest => _pickup.hasCoordinates && _dropoff.hasCoordinates;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: AppColors.background,
      // §4.2 Voice Booking GA: mic FAB gated by the voice_booking flag
      // (default off in production; kill switch documented in
      // config/feature-flags/flags.json). When off the FAB is not built.
      floatingActionButton: _voiceBookingEnabled
          ? FloatingActionButton(
              onPressed: _openVoiceSheet,
              backgroundColor: AppColors.primary,
              tooltip: 'Book with voice',
              child: const Icon(Icons.mic, color: AppColors.textOnDark),
            )
          : null,
      appBar: AppBar(
        title: const Text('Request a Ride'),
        backgroundColor: Colors.white,
        elevation: 0,
        leading: IconButton(
          icon: const Icon(Icons.arrow_back, color: AppColors.textPrimary),
          onPressed: () => context.pop(),
        ),
        actions: [
          // Notification inbox bell with live unread badge (§1.4).
          _NotificationBell(),
        ],
      ),
      body: BlocConsumer<RideBloc, RideState>(
        listener: _handleRideState,
        builder: (context, state) {
          return Column(
            children: [
              SizedBox(
                height: 280,
                child: Stack(
                  children: [
                    // The map + pickup pin are gated: denied permission shows
                    // the rationale card here, but everything below (dropoff,
                    // ride types, payment) remains fully usable.
                    Positioned.fill(
                      child: LocationPermissionGate(
                        child: LiveMapWidget(
                          key: _mapKey,
                          initialCenter: _cameraCenter,
                          initialZoom: _defaultZoom,
                          interactionMode: MapInteractionMode.full,
                          showRecenterFab: true,
                          followUserLocation: true,
                          recenterTarget: _pickup.hasCoordinates
                              ? LatLng(_pickup.lat!, _pickup.lng!)
                              : null,
                          markers: [
                            if (_pickup.hasCoordinates)
                              MapMarkerModel(
                                id: 'pickup',
                                position: LatLng(_pickup.lat!, _pickup.lng!),
                                kind: MapMarkerKind.pickup,
                                label: 'Pickup',
                              ),
                            // §3.5 stop markers in visit order, numbered so
                            // the rider can confirm the sequence matches the
                            // chip list below the dropoff field.
                            for (var i = 0; i < _stops.length; i++)
                              MapMarkerModel(
                                id: 'stop-${i + 1}',
                                position: LatLng(_stops[i].lat, _stops[i].lng),
                                kind: MapMarkerKind.waypoint,
                                label: 'Stop ${i + 1}',
                              ),
                            if (_dropoff.hasCoordinates)
                              MapMarkerModel(
                                id: 'dropoff',
                                position: LatLng(_dropoff.lat!, _dropoff.lng!),
                                kind: MapMarkerKind.dropoff,
                                label: 'Dropoff',
                              ),
                          ],
                          onPickupPinDragged: _onPickupPinDragged,
                          onCameraIdle: (_) {},
                        ),
                      ),
                    ),
                    if (_gpsUnavailableBanner)
                      Positioned(
                        left: 12,
                        right: 12,
                        bottom: 12,
                        child: _GpsUnavailableBanner(onRetry: _acquirePickup),
                      ),
                  ],
                ),
              ),
              Expanded(
                child: Container(
                  decoration: const BoxDecoration(
                    color: AppColors.surface,
                    borderRadius: BorderRadius.only(
                      topLeft: Radius.circular(24),
                      topRight: Radius.circular(24),
                    ),
                    boxShadow: [
                      BoxShadow(
                        color: Colors.black12,
                        blurRadius: 20,
                        offset: Offset(0, -4),
                      ),
                    ],
                  ),
                  child: SingleChildScrollView(
                    padding: const EdgeInsets.all(24),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: [
                        const Text(
                          'Where to?',
                          style: TextStyle(
                            fontSize: 24,
                            fontWeight: FontWeight.bold,
                            color: AppColors.textPrimary,
                          ),
                        ),
                        const SizedBox(height: 24),

                        LocationInput(
                          label: 'Pickup location',
                          icon: Icons.circle,
                          iconColor: AppColors.success,
                          value: _pickup,
                          locating: _pickupLocating,
                          hint: _gpsUnavailableBanner
                              ? 'Current location (unavailable) — drag the pin'
                              : 'Use current location or drag the pin',
                          onTapField: _openPickupSheet,
                          onUseCurrentLocation: _acquirePickup,
                        ),

                        const SizedBox(height: 12),

                        Container(
                          margin: const EdgeInsets.only(left: 28),
                          height: 20,
                          width: 2,
                          color: AppColors.border,
                        ),

                        const SizedBox(height: 12),

                        LocationInput(
                          label: 'Dropoff location',
                          icon: Icons.circle,
                          iconColor: AppColors.error,
                          value: _dropoff,
                          hint: 'Search a place',
                          onTapField: _openDropoffSheet,
                        ),

                        // §3.5: removable stop chips in visit order plus the
                        // "Add stop" affordance. Hidden entirely when the
                        // ride_stopovers flag is off — no dead button.
                        if (_stopoversEnabled) ...[
                          const SizedBox(height: 12),
                          _buildStopsSection(),
                        ],

                        const SizedBox(height: 32),

                        const Text(
                          'Choose ride type',
                          style: TextStyle(
                            fontSize: 16,
                            fontWeight: FontWeight.w600,
                            color: AppColors.textPrimary,
                          ),
                        ),
                        const SizedBox(height: 16),

                        RideTypeSelector(
                          selectedType: _selectedRideType,
                          onTypeSelected: (type) {
                            setState(() => _selectedRideType = type);
                            if (_canRequest) {
                              context.read<RideBloc>().add(
                                    GetFareEstimate(
                                      pickupLat: _pickup.lat!,
                                      pickupLng: _pickup.lng!,
                                      dropoffLat: _dropoff.lat!,
                                      dropoffLng: _dropoff.lng!,
                                      // §2.6 fix: the estimate now honors the
                                      // selected type instead of ignoring it.
                                      rideType: _selectedRideType,
                                    ),
                                  );
                            }
                          },
                        ),

                        const SizedBox(height: 24),

                        PaymentMethodSelector(
                          selectedPaymentMethodId: _selectedPaymentMethodId,
                          onChanged: (methodId, methodType) {
                            setState(() {
                              _selectedPaymentMethodId = methodId;
                              _selectedPaymentMethodType = methodType;
                            });
                          },
                        ),

                        const SizedBox(height: 16),

                        _buildPromoRow(),

                        const SizedBox(height: 16),

                        _buildScheduleRow(state),

                        const SizedBox(height: 24),

                        if (state is FareEstimateLoaded && _canRequest)
                          _buildFareEstimate(state.estimate),

                        const SizedBox(height: 24),

                        _buildRequestButton(state),

                        if (state is RideLoading) ...[
                          const SizedBox(height: 16),
                          const Center(
                            child: CircularProgressIndicator(
                              color: AppColors.primary,
                            ),
                          ),
                        ],

                        if (state is RideError) ...[
                          const SizedBox(height: 16),
                          _buildErrorMessage(state.message),
                        ],
                      ],
                    ),
                  ),
                ),
              ),
            ],
          );
        },
      ),
    );
  }

  /// §3.5 stopover editor: numbered removable chips in visit order followed
  /// by an "Add stop" row (hidden once the cap is reached — same number the
  /// backend enforces). Each row opens the existing place-autocomplete sheet;
  /// choosing a suggestion appends the stop, re-fits the camera across all
  /// waypoints, and refreshes the fare estimate so the rider sees the
  /// multi-leg number before booking (the server recomputes authoritatively
  /// at request time regardless).
  Widget _buildStopsSection() {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        for (var i = 0; i < _stops.length; i++)
          Padding(
            padding: const EdgeInsets.only(bottom: 8),
            child: InputChip(
              avatar: CircleAvatar(
                backgroundColor: const Color(0xFFF9A825),
                child: Text(
                  '${i + 1}',
                  style: const TextStyle(
                    // §4.4: intentional white-on-amber foreground.
                    color: AppColors.textOnDark,
                    fontWeight: FontWeight.bold,
                    fontSize: 12,
                  ),
                ),
              ),
              label: Text(
                _stops[i].address?.isNotEmpty == true
                    ? _stops[i].address!
                    : 'Stop ${i + 1}',
                overflow: TextOverflow.ellipsis,
              ),
              onDeleted: () => _removeStop(i),
              deleteIcon: const Icon(Icons.close, size: 18),
            ),
          ),
        if (_stops.length < maxRideStops)
          Material(
            color: AppColors.surfaceMuted ?? AppColors.surface,
            borderRadius: BorderRadius.circular(12),
            child: InkWell(
              borderRadius: BorderRadius.circular(12),
              onTap: _addStop,
              child: const Padding(
                padding: EdgeInsets.symmetric(horizontal: 14, vertical: 12),
                child: Row(
                  children: [
                    Icon(
                      Icons.add_location_alt_outlined,
                      size: 20,
                      color: AppColors.textSecondary,
                    ),
                    SizedBox(width: 10),
                    Text(
                      'Add stop',
                      style: TextStyle(
                        fontSize: 14,
                        color: AppColors.textPrimary,
                      ),
                    ),
                    Spacer(),
                    Text(
                      '$maxRideStops max',
                      style: TextStyle(
                        fontSize: 12,
                        color: AppColors.textSecondary,
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
      ],
    );
  }

  Future<void> _addStop() async {
    // Cap check mirrors entities.MaxRideStops / TOO_MANY_STOPS wording so a
    // race between rebuilds can never produce a fourth stop.
    if (_stops.length >= maxRideStops) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(
              'A maximum of $maxRideStops stops per ride is allowed.',
            ),
          ),
        );
      }
      return;
    }
    final outcome = await PlaceAutocompleteSheet.show(
      context,
      geocoding: _geocoding,
      title: 'Add stop ${_stops.length + 1}',
      bias: _pickup.hasCoordinates
          ? LatLng(_pickup.lat!, _pickup.lng!)
          : null,
    );
    if (!mounted) return;
    if (outcome.kind != PlaceAutocompleteOutcomeKind.suggestion) return;
    final s = outcome.suggestion!;
    setState(() {
      _stops.add(RideStop(
        seq: _stops.length + 1,
        lat: s.location.latitude,
        lng: s.location.longitude,
        address: s.detail.isEmpty ? s.name : '${s.name}, ${s.detail}',
      ));
    });
    _fitCameraToPoints();
    _refreshFareEstimate();
  }

  void _removeStop(int index) {
    setState(() {
      _stops.removeAt(index);
      // Re-number in visit order — seq must stay 1..n contiguous to match
      // the backend's UNIQUE(ride_id, seq) contract when the request ships.
      for (var i = 0; i < _stops.length; i++) {
        _stops[i] = RideStop(
          seq: i + 1,
          lat: _stops[i].lat,
          lng: _stops[i].lng,
          address: _stops[i].address,
        );
      }
    });
    _fitCameraToPoints();
    _refreshFareEstimate();
  }

  /// Re-runs the fare estimate after any waypoint change. The estimate
  /// endpoint stays pickup→dropoff (a display projection only); the
  /// authoritative multi-leg fare is computed by the pricing service over
  /// the full chain at POST /rides time.
  void _refreshFareEstimate() {
    if (!_canRequest) return;
    context.read<RideBloc>().add(
          GetFareEstimate(
            pickupLat: _pickup.lat!,
            pickupLng: _pickup.lng!,
            dropoffLat: _dropoff.lat!,
            dropoffLng: _dropoff.lng!,
            rideType: _selectedRideType,
          ),
        );
  }

  Future<void> _openPickupSheet() async {
    final outcome = await PlaceAutocompleteSheet.show(
      context,
      geocoding: _geocoding,
      title: 'Set pickup location',
      bias: _pickup.hasCoordinates
          ? LatLng(_pickup.lat!, _pickup.lng!)
          : null,
    );
    if (!mounted) return;
    switch (outcome.kind) {
      case PlaceAutocompleteOutcomeKind.suggestion:
        _applySuggestion(isPickup: true, suggestion: outcome.suggestion!);
        break;
      case PlaceAutocompleteOutcomeKind.currentLocation:
        _acquirePickup();
        break;
      case PlaceAutocompleteOutcomeKind.setPin:
        // Enter pin-drag mode: recenter on the current camera target and let
        // the rider long-press-drag the pickup marker (LiveMapWidget owns the
        // drag gesture; the label resolves via reverse geocode on release).
        _mapKey.currentState?.moveTo(_cameraCenter);
        break;
      case PlaceAutocompleteOutcomeKind.none:
        break;
    }
  }

  Future<void> _openDropoffSheet() async {
    final outcome = await PlaceAutocompleteSheet.show(
      context,
      geocoding: _geocoding,
      title: 'Set dropoff location',
      bias: _pickup.hasCoordinates
          ? LatLng(_pickup.lat!, _pickup.lng!)
          : null,
    );
    if (!mounted) return;
    switch (outcome.kind) {
      case PlaceAutocompleteOutcomeKind.suggestion:
        _applySuggestion(isPickup: false, suggestion: outcome.suggestion!);
        break;
      case PlaceAutocompleteOutcomeKind.currentLocation:
      case PlaceAutocompleteOutcomeKind.setPin:
        // Those rows only make sense for pickup; for dropoff they behave like
        // a no-op close so nothing silently mislabels the destination field.
        break;
      case PlaceAutocompleteOutcomeKind.none:
        break;
    }
  }

  /// A chosen suggestion stores both the resolved LatLng and its display
  /// string — geocoding happened at selection time inside the sheet chain, so
  /// no deferred address-resolution step exists anywhere downstream.
  void _applySuggestion({
    required bool isPickup,
    required PlaceSuggestion suggestion,
  }) {
    setState(() {
      final value = LocationFieldValue(
        display: suggestion.detail.isEmpty
            ? suggestion.name
            : '${suggestion.name}, ${suggestion.detail}',
        lat: suggestion.location.latitude,
        lng: suggestion.location.longitude,
      );
      if (isPickup) {
        _pickup = value;
        _gpsUnavailableBanner = false;
      } else {
        _dropoff = value;
      }
    });
    _fitCameraToPoints();
    if (_canRequest) {
      context.read<RideBloc>().add(
            GetFareEstimate(
              pickupLat: _pickup.lat!,
              pickupLng: _pickup.lng!,
              dropoffLat: _dropoff.lat!,
              dropoffLng: _dropoff.lng!,
              rideType: _selectedRideType,
            ),
          );
    }
  }

  Widget _buildFareEstimate(dynamic estimate) {
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: AppColors.primary.withOpacity(0.05),
        borderRadius: BorderRadius.circular(16),
        border: Border.all(
          color: AppColors.primary.withOpacity(0.2),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            mainAxisAlignment: MainAxisAlignment.spaceBetween,
            children: [
              const Text(
                'Estimated Fare',
                style: TextStyle(
                  fontSize: 14,
                  color: AppColors.textSecondary,
                ),
              ),
              Text(
                '\$${estimate.totalFare.toStringAsFixed(2)}',
                style: const TextStyle(
                  fontSize: 24,
                  fontWeight: FontWeight.bold,
                  color: AppColors.primary,
                ),
              ),
            ],
          ),
          const SizedBox(height: 8),
          Row(
            children: [
              Icon(
                Icons.directions_car,
                size: 16,
                color: AppColors.textSecondary,
              ),
              const SizedBox(width: 4),
              Text(
                '${estimate.distanceKm.toStringAsFixed(1)} km',
                style: TextStyle(
                  fontSize: 12,
                  color: AppColors.textSecondary,
                ),
              ),
              const SizedBox(width: 16),
              Icon(
                Icons.access_time,
                size: 16,
                color: AppColors.textSecondary,
              ),
              const SizedBox(width: 4),
              Text(
                '${estimate.durationMinutes} min',
                style: TextStyle(
                  fontSize: 12,
                  color: AppColors.textSecondary,
                ),
              ),
              if (estimate.surgeMultiplier > 1.0) ...[
                const SizedBox(width: 16),
                Icon(
                  Icons.trending_up,
                  size: 16,
                  color: AppColors.warning,
                ),
                const SizedBox(width: 4),
                Text(
                  '${estimate.surgeMultiplier}x surge',
                  style: const TextStyle(
                    fontSize: 12,
                    color: AppColors.warning,
                    fontWeight: FontWeight.w600,
                  ),
                ),
              ],
            ],
          ),
        ],
      ),
    );
  }

  Widget _buildRequestButton(RideState state) {
    final isLoading = state is RideLoading;

    return SizedBox(
      height: 56,
      child: ElevatedButton(
        onPressed: isLoading || !_canRequest ? null : _requestRide,
        style: ElevatedButton.styleFrom(
          backgroundColor: AppColors.primary,
          foregroundColor: AppColors.textOnDark,
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(16),
          ),
          elevation: 0,
        ),
        child: isLoading
            ? const SizedBox(
                width: 24,
                height: 24,
                child: CircularProgressIndicator(
                  strokeWidth: 2.5,
                  valueColor: const AlwaysStoppedAnimation<Color>(AppColors.textOnDark),
                ),
              )
            : Row(
                mainAxisAlignment: MainAxisAlignment.center,
                children: [
                  const Icon(Icons.directions_car, size: 20),
                  const SizedBox(width: 8),
                  Text(
                    _canRequest ? 'Request Ride' : 'Set pickup & dropoff',
                    style: const TextStyle(
                      fontSize: 16,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                ],
              ),
      ),
    );
  }

  Widget _buildErrorMessage(String message) {
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: AppColors.error.withOpacity(0.1),
        borderRadius: BorderRadius.circular(12),
        border: Border.all(
          color: AppColors.error.withOpacity(0.3),
        ),
      ),
      child: Row(
        children: [
          const Icon(Icons.error_outline, color: AppColors.error, size: 20),
          const SizedBox(width: 12),
          Expanded(
            child: Text(
              message,
              style: const TextStyle(
                color: AppColors.error,
                fontSize: 14,
              ),
            ),
          ),
        ],
      ),
    );
  }

  void _requestRide() {
    if (!_canRequest) return;
    context.read<RideBloc>().add(
          RequestRide(
            pickupLat: _pickup.lat!,
            pickupLng: _pickup.lng!,
            dropoffLat: _dropoff.lat!,
            dropoffLng: _dropoff.lng!,
            pickupAddress: _pickup.display,
            dropoffAddress: _dropoff.display,
            rideType: _selectedRideType,
            paymentMethodId: _selectedPaymentMethodId,
            // §3.1 / §3.2 / §3.6: the registry rail type, the validated promo
            // code and the scheduled pickup time now travel with the request.
            // Each is null unless genuinely chosen — the backend resolves
            // defaults rather than the client fabricating them.
            paymentMethodType: _selectedPaymentMethodType,
            promoCode: _promoCode,
            scheduledAt: _scheduledAt,
            // §3.5: stops travel with the request; empty list sends no key.
            stops: List<RideStop>.unmodifiable(_stops),
          ),
        );
  }

  // ==========================================================================
  // §3.2 Promo row + §3.6 Schedule toggle (Request page)
  // ==========================================================================

  /// Estimated fare currently on screen, used as the redeem projection input.
  /// Null until a real estimate has loaded — never guessed.
  double? get _currentEstimatedFare {
    final state = context.read<RideBloc>().state;
    if (state is FareEstimateLoaded && _canRequest) {
      return state.estimate.totalFare;
    }
    return null;
  }

  String get _estimateCurrency {
    final state = context.read<RideBloc>().state;
    if (state is FareEstimateLoaded) return state.estimate.currency;
    return 'ETB';
  }

  /// "Promo code" row that opens [PromoCodeSheet]. Hidden entirely when the
  /// FEATURE_PROMOTIONS client flag is off (mirrors the backend kill switch).
  Widget _buildPromoRow() {
    if (!getIt<FeatureFlagService>().isEnabled('promotions')) {
      return const SizedBox.shrink();
    }
    final hasCode = _promoCode != null;
    return Material(
      color: AppColors.surfaceMuted ?? AppColors.surface,
      borderRadius: BorderRadius.circular(12),
      child: InkWell(
        borderRadius: BorderRadius.circular(12),
        onTap: () => PromoCodeSheet.show(
          context,
          estimatedFare: _currentEstimatedFare,
          currency: _estimateCurrency,
          rideType: _selectedRideType,
          currentCode: _promoCode,
          onApplied: (code, discount) {
            setState(() {
              _promoCode = code;
              _projectedDiscount = discount;
            });
          },
        ),
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
          child: Row(
            children: [
              const Icon(
                Icons.local_offer_outlined,
                size: 20,
                color: AppColors.textSecondary,
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Text(
                  hasCode
                      ? '${_promoCode!} applied'
                      : 'Add promo code',
                  style: TextStyle(
                    fontSize: 14,
                    fontWeight: hasCode ? FontWeight.w700 : FontWeight.w400,
                    color: hasCode
                        ? AppColors.primary
                        : AppColors.textPrimary,
                  ),
                ),
              ),
              // Server-projected discount only — shown after a successful
              // POST /promotions/redeem, never computed on-device.
              if (hasCode && _projectedDiscount != null)
                Text(
                  '-${_estimateCurrency} ${_projectedDiscount!.toStringAsFixed(2)}',
                  style: const TextStyle(
                    fontSize: 13,
                    fontWeight: FontWeight.w700,
                    color: AppColors.success,
                  ),
                )
              else if (hasCode)
                const Text(
                  'discount confirmed at booking',
                  style: TextStyle(fontSize: 12, color: AppColors.textSecondary),
                ),
              const SizedBox(width: 6),
              const Icon(Icons.chevron_right, size: 20),
            ],
          ),
        ),
      ),
    );
  }

  /// §3.6 "Schedule" toggle with a date-time picker constrained to
  /// ≥15 min from now and ≤30 days out — the same window the backend
  /// enforces, so the picker cannot offer a time the server would reject.
  Widget _buildScheduleRow(RideState state) {
    final isScheduled = _scheduledAt != null;
    return Material(
      color: AppColors.surfaceMuted ?? AppColors.surface,
      borderRadius: BorderRadius.circular(12),
      child: InkWell(
        borderRadius: BorderRadius.circular(12),
        onTap: _pickSchedule,
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
          child: Row(
            children: [
              const Text(
                'Schedule',
                style: TextStyle(fontSize: 15, fontWeight: FontWeight.w700),
              ),
              Switch(
                value: isScheduled,
                activeColor: AppColors.primary,
                onChanged: (on) {
                  if (on) {
                    _pickSchedule();
                  } else {
                    setState(() => _scheduledAt = null);
                  }
                },
              ),
              Expanded(
                child: Text(
                  isScheduled
                      ? _formatScheduled(_scheduledAt!)
                      : 'Now (on-demand)',
                  style: const TextStyle(
                    fontSize: 13,
                    color: AppColors.textSecondary,
                  ),
                  textAlign: TextAlign.right,
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Future<void> _pickSchedule() async {
    // Backend window (§1/§3.6): first selectable moment is now + 15 min,
    // last is now + 30 days. Mirror it here so users never see an option
    // that would come back SCHEDULED_TOO_SOON / SCHEDULED_TOO_FAR.
    final now = DateTime.now();
    final earliest = now.add(const Duration(minutes: 15));
    final latest = now.add(const Duration(days: 30));

    final date = await showDatePicker(
      context: context,
      initialDate: _scheduledAt != null &&
              _scheduledAt!.isAfter(earliest) &&
              _scheduledAt!.isBefore(latest)
          ? _scheduledAt!
          : earliest,
      firstDate: earliest,
      lastDate: latest,
    );
    if (date == null || !mounted) return;

    final time = await showTimePicker(
      context: context,
      initialTime: TimeOfDay.fromDateTime(
        _scheduledAt ?? earliest,
      ),
    );
    if (time == null || !mounted) return;

    final picked = DateTime(
      date.year,
      date.month,
      date.day,
      time.hour,
      time.minute,
    );

    if (picked.isBefore(earliest)) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(
            content: Text(
              'Scheduled pickups must be at least 15 minutes away.',
            ),
          ),
        );
      }
      return;
    }
    if (picked.isAfter(latest)) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(
            content: Text('Scheduled pickups can go up to 30 days ahead.'),
          ),
        );
      }
      return;
    }
    setState(() => _scheduledAt = picked);
  }

  String _formatScheduled(DateTime t) {
    final d = '${t.day.toString().padLeft(2, '0')}/'
        '${t.month.toString().padLeft(2, '0')}';
    final hm = '${t.hour.toString().padLeft(2, '0')}:'
        '${t.minute.toString().padLeft(2, '0')}';
    return '$d $hm';
  }

  // ==========================================================================
  // §4.2 Voice Booking GA (Request page mic FAB)
  // ==========================================================================

  /// Opens the listening sheet. The recognizer is initialized lazily here so
  /// the mic permission prompt only fires when the rider actually taps the
  /// FAB. A failed initialization shows an honest "unavailable" state — never
  /// a fake listening animation.
  Future<void> _openVoiceSheet() async {
    final service = VoiceCommandService();
    await showModalBottomSheet<void>(
      context: context,
      backgroundColor: AppColors.surface,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
      ),
      builder: (sheetContext) => _VoiceListeningSheet(
        service: service,
        onCommand: (command) async {
          // Pop the listening sheet first; all follow-up surfaces (autocomplete
          // fallback, snackbar) attach to the page context afterwards.
          if (Navigator.of(sheetContext).canPop()) {
            Navigator.of(sheetContext).pop();
          }
          await _handleVoiceCommand(command);
        },
      ),
    );
  }

  /// Routes a parsed [VoiceCommand] into the SAME flows the manual UI uses.
  Future<void> _handleVoiceCommand(VoiceCommand command) async {
    switch (command.type) {
      case CommandType.bookRide:
        final destination =
            (command.parameters['destination'] as String? ?? '').trim();
        if (destination.isEmpty) {
          _showVoiceSnack('No destination heard — tap the dropoff field or '
              'say "ride to <place>".');
          return;
        }
        if (!command.isHighConfidence) {
          // Low confidence: open the autocomplete sheet prefilled with the
          // transcript so the rider confirms/corrects before anything is set.
          final outcome = await PlaceAutocompleteSheet.show(
            context,
            geocoding: _geocoding,
            title: 'Confirm your destination',
            bias: _pickup.hasCoordinates
                ? LatLng(_pickup.lat!, _pickup.lng!)
                : null,
            initialQuery: destination,
          );
          if (!mounted) return;
          if (outcome.kind == PlaceAutocompleteOutcomeKind.suggestion) {
            _applySuggestion(isPickup: false, suggestion: outcome.suggestion!);
          }
          return;
        }
        // High confidence: resolve through the same geocoding chain the typed
        // path uses. Zero matches must NOT silently book — fall back to the
        // prefilled sheet instead of guessing a place.
        List<PlaceSuggestion> matches;
        try {
          matches = await _geocoding.autocomplete(
            destination,
            bias: _pickup.hasCoordinates
                ? LatLng(_pickup.lat!, _pickup.lng!)
                : null,
            limit: 3,
          );
        } catch (_) {
          if (!mounted) return;
          _showVoiceSnack('Location search unavailable right now — pick your '
              'destination manually.');
          final outcome = await PlaceAutocompleteSheet.show(
            context,
            geocoding: _geocoding,
            title: 'Set dropoff location',
            bias: _pickup.hasCoordinates
                ? LatLng(_pickup.lat!, _pickup.lng!)
                : null,
            initialQuery: destination,
          );
          if (!mounted) return;
          if (outcome.kind == PlaceAutocompleteOutcomeKind.suggestion) {
            _applySuggestion(isPickup: false, suggestion: outcome.suggestion!);
          }
          return;
        }
        if (!mounted) return;
        if (matches.isEmpty) {
          _showVoiceSnack('"$destination" didn\'t match any place — pick it '
              'from the list.');
          final outcome = await PlaceAutocompleteSheet.show(
            context,
            geocoding: _geocoding,
            title: 'Set dropoff location',
            bias: _pickup.hasCoordinates
                ? LatLng(_pickup.lat!, _pickup.lng!)
                : null,
            initialQuery: destination,
          );
          if (!mounted) return;
          if (outcome.kind == PlaceAutocompleteOutcomeKind.suggestion) {
            _applySuggestion(isPickup: false, suggestion: outcome.suggestion!);
          }
          return;
        }
        // Single resolved destination -> exactly what tapping a suggestion in
        // the sheet would do; the rider still presses "Request" to book.
        _applySuggestion(isPickup: false, suggestion: matches.first);
        _showVoiceSnack('Destination set to ${matches.first.name}. Review and '
            'tap Request when ready.');
        break;

      case CommandType.cancelRide:
      case CommandType.driverLocation:
        // Only meaningful mid-ride; on the request page they are no-ops with
        // honest guidance rather than dead actions.
        _showVoiceSnack('You have no active ride right now.');
        break;

      case CommandType.error:
        _showVoiceSnack(command.feedback);
        break;

      case CommandType.noMatch:
      case CommandType.help:
        _showVoiceSnack(command.feedback);
        break;

      // Non-ride intents (hotel/food/navigate/AR/home) belong to other
      // modules; the request page does not fake-handle them. Exhaustive
      // listing (no default) so new CommandType values fail compilation
      // here instead of silently falling through.
      case CommandType.findHotel:
      case CommandType.orderFood:
      case CommandType.navigate:
      case CommandType.enableAR:
      case CommandType.goHome:
        _showVoiceSnack('That command isn\'t available while requesting a '
            'ride.');
        break;
    }
  }

  void _showVoiceSnack(String message) {
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text(message), duration: const Duration(seconds: 4)),
    );
  }

  void _handleRideState(BuildContext context, RideState state) {
    if (state is RideRequested) {
      context.go('/nidus/tracking/${state.rideId}');
    }
  }
}

/// §4.2: listening surface behind the mic FAB. Shows live transcript, honest
/// unavailable/error states (no fake pulse animation when the recognizer is
/// dead), and forwards every finalized [VoiceCommand] to the page for routing.
class _VoiceListeningSheet extends StatefulWidget {
  final VoiceCommandService service;
  final Future<void> Function(VoiceCommand command) onCommand;

  const _VoiceListeningSheet({
    required this.service,
    required this.onCommand,
  });

  @override
  State<_VoiceListeningSheet> createState() => _VoiceListeningSheetState();
}

class _VoiceListeningSheetState extends State<_VoiceListeningSheet> {
  StreamSubscription<VoiceCommand>? _sub;
  String _transcript = '';
  String? _unavailableReason;
  bool _starting = true;

  @override
  void initState() {
    super.initState();
    _start();
  }

  Future<void> _start() async {
    // Commands emitted during initialize()/listen() startup are consumed via
    // the broadcast stream subscribed below; subscribe first to avoid missing
    // an early error event.
    _sub = widget.service.commandStream.listen(_onCommand);
    final ok = await widget.service.startListening();
    if (!mounted) return;
    setState(() {
      _starting = false;
      if (!ok) {
        _unavailableReason = 'Voice input unavailable — microphone permission '
            'denied or recognizer failed to start.';
      }
    });
  }

  void _onCommand(VoiceCommand command) {
    if (!mounted) return;
    setState(() => _transcript = widget.service.lastCommand);
    // Only ride-relevant + feedback-worthy commands close the sheet; the page
    // owns what happens next (autocomplete fallback, destination fill, etc.).
    widget.onCommand(command);
  }

  @override
  void dispose() {
    _sub?.cancel();
    widget.service.stopListening();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            const Text(
              'Speak your ride request',
              style: TextStyle(
                fontSize: 18,
                fontWeight: FontWeight.w600,
                color: AppColors.textPrimary,
              ),
            ),
            const SizedBox(height: 8),
            const Text(
              'Try "ride to Bole Airport" or say "help" for examples.',
              style: TextStyle(fontSize: 13, color: AppColors.textSecondary),
            ),
            const SizedBox(height: 20),
            if (_unavailableReason != null)
              Row(
                children: [
                  const Icon(Icons.mic_off,
                      color: AppColors.error, size: 20),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Text(
                      _unavailableReason!,
                      style: const TextStyle(
                          color: AppColors.error, fontSize: 14),
                    ),
                  ),
                ],
              )
            else ...[
              Row(
                children: [
                  Icon(
                    _starting ? Icons.hourglass_top : Icons.mic,
                    color: AppColors.primary,
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Text(
                      _starting
                          ? 'Starting microphone…'
                          : (_transcript.isEmpty
                              ? 'Listening…'
                              : '"$_transcript"'),
                      style: const TextStyle(
                        fontSize: 15,
                        color: AppColors.textPrimary,
                      ),
                    ),
                  ),
                ],
              ),
            ],
            const SizedBox(height: 20),
            TextButton(
              onPressed: () => Navigator.of(context).pop(),
              child: const Text('Close'),
            ),
          ],
        ),
      ),
    );
  }
}

/// Honest "GPS unavailable" strip with retry — shown when the service is off
/// or the fix timed out, never replacing the map with a blank area.
class _GpsUnavailableBanner extends StatelessWidget {
  final VoidCallback onRetry;

  const _GpsUnavailableBanner({required this.onRetry});

  @override
  Widget build(BuildContext context) {
    return Material(
      color: AppColors.warning,
      borderRadius: BorderRadius.circular(10),
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
        child: Row(
          children: [
            const Icon(Icons.gps_off, color: AppColors.textOnDark, size: 18),
            const SizedBox(width: 8),
            const Expanded(
              child: Text(
                'GPS unavailable — drag the pickup pin to set your location.',
                style: const TextStyle(color: AppColors.textOnDark, fontSize: 12),
              ),
            ),
            TextButton(
              onPressed: onRetry,
              child: const Text(
                'Retry',
                style: TextStyle(
                  color: AppColors.textOnDark,
                  fontWeight: FontWeight.bold,
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// App-bar bell with a live unread badge (§1.4 step 7). Subscribes to
/// [NotificationService.unreadStream]; tapping opens the inbox route. The
/// badge disappears entirely at zero — no fake "0" chip.
class _NotificationBell extends StatelessWidget {
  const _NotificationBell();

  @override
  Widget build(BuildContext context) {
    final service = getIt<NotificationService>();
    return StreamBuilder<int>(
      stream: service.unreadStream,
      initialData: service.unreadCount,
      builder: (context, snapshot) {
        final unread = snapshot.data ?? 0;
        return IconButton(
          icon: Badge(
            isLabelVisible: unread > 0,
            label: Text(unread > 99 ? '99+' : '$unread'),
            child: const Icon(
              Icons.notifications_outlined,
              color: AppColors.textPrimary,
            ),
          ),
          tooltip: unread > 0
              ? 'Notifications ($unread unread)'
              : 'Notifications',
          onPressed: () => context.go('/nidus/notifications'),
        );
      },
    );
  }
}
