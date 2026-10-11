import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:go_router/go_router.dart';
import 'package:latlong2/latlong.dart';

import 'package:get_it/get_it.dart';

import '../../../../core/di/injection.dart';
import '../../../../core/geocoding/geocoding_models.dart';
import '../../../../core/geocoding/geocoding_service.dart';
import '../../../../core/location/device_location_service.dart';
import '../../../../core/map/live_map_widget.dart';
import '../../../../core/theme/app_colors.dart';
import '../bloc/ride_bloc.dart';
import '../widgets/location_input.dart';
import '../widgets/location_permission_gate.dart';
import '../widgets/place_autocomplete_sheet.dart';
import '../widgets/payment_method_selector.dart';
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
    final points = <LatLng>[
      if (_pickup.hasCoordinates) LatLng(_pickup.lat!, _pickup.lng!),
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
      appBar: AppBar(
        title: const Text('Request a Ride'),
        backgroundColor: Colors.white,
        elevation: 0,
        leading: IconButton(
          icon: const Icon(Icons.arrow_back, color: AppColors.textPrimary),
          onPressed: () => context.pop(),
        ),
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
                    color: Colors.white,
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
                                    ),
                                  );
                            }
                          },
                        ),

                        const SizedBox(height: 24),

                        PaymentMethodSelector(
                          selectedPaymentMethodId: _selectedPaymentMethodId,
                          onChanged: (methodId) {
                            setState(() => _selectedPaymentMethodId = methodId);
                          },
                        ),

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
  /// there is no deferred "// TODO: Geocode" anywhere downstream.
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
          foregroundColor: Colors.white,
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
                  valueColor: AlwaysStoppedAnimation<Color>(Colors.white),
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
          ),
        );
  }

  void _handleRideState(BuildContext context, RideState state) {
    if (state is RideRequested) {
      context.go('/nidus/tracking/${state.rideId}');
    }
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
            const Icon(Icons.gps_off, color: Colors.white, size: 18),
            const SizedBox(width: 8),
            const Expanded(
              child: Text(
                'GPS unavailable — drag the pickup pin to set your location.',
                style: TextStyle(color: Colors.white, fontSize: 12),
              ),
            ),
            TextButton(
              onPressed: onRetry,
              child: const Text(
                'Retry',
                style: TextStyle(
                  color: Colors.white,
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
