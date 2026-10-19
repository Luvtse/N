import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:latlong2/latlong.dart';

import '../../../../core/config/feature_flag_service.dart';
import '../../../../core/di/injection.dart';
import '../../../../core/geocoding/geocoding_service.dart';
import '../../../../core/map/live_map_widget.dart';
import '../../../../core/theme/app_colors.dart';
import '../bloc/ride_bloc.dart';
import '../../data/repositories/ride_repository.dart'
    show Ride, RideRepository, DestinationChangeResult;
import '../widgets/cancel_ride_sheet.dart';
import '../widgets/place_autocomplete_sheet.dart';
import '../widgets/driver_card.dart';
import '../widgets/payment_method_selector.dart';
import '../widgets/pre_selfie_capture_sheet.dart';
import '../widgets/rate_ride_sheet.dart';
import '../widgets/safety_widgets.dart';
import '../widgets/verify_driver_card.dart';
import 'chat_page.dart';
import 'receipt_detail_page.dart';
import 'safety_toolkit_page.dart';

/// Tracking surface rebuilt on the Foundation map (§1.1):
/// - driver marker animates between successive position updates with an
///   800 ms TweenAnimationBuilder;
/// - route polyline drawn through [LiveMapWidget]'s PolylineLayer;
/// - camera follows the rider while the driver is en route and switches to
///   following the driver marker once the ride is in progress;
/// - the Recenter FAB toggles between following driver and following rider.
class RideTrackingPage extends StatefulWidget {
  final String rideId;

  const RideTrackingPage({super.key, required this.rideId});

  @override
  State<RideTrackingPage> createState() => _RideTrackingPageState();
}

class _RideTrackingPageState extends State<RideTrackingPage>
    with SingleTickerProviderStateMixin {
  static const Duration _driverAnimationDuration = Duration(milliseconds: 800);

  /// Camera-only fallback center (Addis Ababa region, matching the product's
  /// market). Never rendered as a pin or address — it just keeps the camera
  /// out of the ocean until the first real coordinate arrives via WS.
  static final LatLng _cameraFallback = LatLng(9.03, 38.74);

  final GlobalKey<LiveMapWidgetState> _mapKey =
      GlobalKey<LiveMapWidgetState>();

  /// Previous driver fix — the tween start point for the next update.
  LatLng? _previousDriverPoint;

  /// Newest committed driver fix — the tween end point.
  LatLng? _committedDriverPoint;

  /// True while the camera follows the driver marker; false = follow rider.
  bool _followDriver = true;

  /// §2.3: guards the one-shot auto-open of the driver pre-selfie capture
  /// sheet. Reset when leaving DriverEnRouteForDriver so a re-entry (e.g.
  /// after a reconnect) can prompt again if the driver still hasn't sent.
  bool _preSelfieSheetShown = false;

  @override
  void initState() {
    super.initState();
    // During DriverEnRoute the plan says followUserLocation: true (camera
    // tracks the rider heading to the pin); during RideInProgress it flips to
    // false and the camera follows the driver marker instead. We default to
    // the in-trip behavior and adjust from state transitions in build.
    context.read<RideBloc>().add(SubscribeToRideUpdates(widget.rideId));
  }

  @override
  void dispose() {
    super.dispose();
  }

  /// Commits a fresh WS fix, remembering the previous point so the marker can
  /// tween toward the new one over [_driverAnimationDuration] instead of
  /// teleporting. Call once per build when [target] differs from the last
  /// committed fix.
  void _commitDriverFix(LatLng target) {
    if (_committedDriverPoint == target) return;
    _previousDriverPoint = _committedDriverPoint ?? target;
    _committedDriverPoint = target;
  }

  /// Driver marker widget: a TweenAnimationBuilder interpolates between the
  /// previous and newest fixes and moves the map imperatively each frame
  /// (plan §1.1: smooth with a TweenAnimationBuilder over 800 ms).
  Widget _driverMarker(LatLng latest) {
    final start = _previousDriverPoint ?? latest;
    return TweenAnimationBuilder<LatLng>(
      key: ValueKey('driver-tween-$latest.latitude-$latest.longitude'),
      tween: Tween<LatLng>(begin: start, end: latest),
      duration: _driverAnimationDuration,
      curve: Curves.linear,
      builder: (context, interpolated, _) {
        WidgetsBinding.instance.addPostFrameCallback((_) {
          _mapKey.currentState?.moveTo(interpolated);
        });
        return const SizedBox.shrink();
      },
    );
  }

  LatLng? _driverTargetFrom(RideState state) {
    if (state is DriverMatched) {
      return LatLng(state.driver.currentLat, state.driver.currentLng);
    }
    if (state is DriverEnRoute) {
      return LatLng(state.driver.currentLat, state.driver.currentLng);
    }
    if (state is RideInProgress) {
      final d = state.ride.driver;
      if (d != null) return LatLng(d.currentLat, d.currentLng);
    }
    return null;
  }

  List<LatLng> _routePoints(RideState state) {
    final pickup = _pickupOf(state);
    final dropoff = _dropoffOf(state);
    final driver = _driverTargetFrom(state);
    if (pickup == null || dropoff == null) return const [];
    // Leg already travelled: pickup -> current driver position; remaining leg:
    // driver -> dropoff. Straight legs until OSRM routing is wired (§backend).
    if (driver == null) return [pickup, dropoff];
    return [pickup, driver, dropoff];
  }

  LatLng? _pickupOf(RideState state) {
    if (state is RideRequested) return null;
    if (state is DriverMatched || state is DriverEnRoute) {
      // Coordinates live on the ride record; the bloc re-emits them via the
      // subscribed updates. Until the first update lands we have no fabricated
      // fallback — returning null simply renders fewer layers.
      return null;
    }
    if (state is RideInProgress || state is RideCompleted) {
      return LatLng(state.ride.pickupLat, state.ride.pickupLng);
    }
    return null;
  }

  LatLng? _dropoffOf(RideState state) {
    if (state is RideInProgress || state is RideCompleted) {
      return LatLng(state.ride.dropoffLat, state.ride.dropoffLng);
    }
    return null;
  }

  @override
  Widget build(BuildContext context) {
    return BlocConsumer<RideBloc, RideState>(
      listenWhen: (previous, current) => previous != current,
      listener: (context, state) {
        // §2.3 driver-side trigger: when THIS session enters DriverEnRoute
        // as the ride's driver, fire the pre-selfie capture step exactly
        // once per en-route phase (plan: "capture step that fires on
        // DriverEnRoute start"). The sheet itself offers retry on upload
        // failure and an explicit skip; skipping leaves the rider with the
        // honest "Driver did not send a selfie" state — never a fake image.
        if (state is DriverEnRouteForDriver) {
          if (!_preSelfieSheetShown) {
            _preSelfieSheetShown = true;
            PreSelfieCaptureSheet.show(context, rideId: state.rideId);
          }
        } else {
          _preSelfieSheetShown = false;
        }
        if (state is RideCompleted) {
          RateRideSheet.show(
            context,
            rideId: state.ride.id,
            fare: state.ride.fareAmount ?? 0,
            currency: state.ride.currency,
          );
        } else if (state is PaymentFailed) {
          PaymentFailureSheet.show(
            context,
            message: state.message,
            errorCode: state.errorCode,
          );
        } else if (state is RideCancelled ||
            state is NoDriversFound ||
            state is RideError) {
          // Terminal / recovery states live on their own surfaces; leave the
          // tracking page for the request flow so the rider can re-book.
          Navigator.of(context).pop();
        }
      },
      builder: (context, state) {
        final driverTarget = _driverTargetFrom(state);
        if (driverTarget != null) _commitDriverFix(driverTarget);
        final animated = driverTarget;
        final route = _routePoints(state);
        final inTrip = state is RideInProgress;

        // Camera policy per §1.1: follow rider while en route to pickup,
        // follow the driver marker during the trip. The FAB lets the rider
        // override either way.
        final followRider = !inTrip && !_followDriver;
        final cameraTarget = _followDriver
            ? animated
            : (_pickupOf(state) ?? animated);

        return Scaffold(
          body: Stack(
            children: [
              // Live OSM map (replaces the deleted placeholder MapWidget).
              Positioned.fill(
                child: LiveMapWidget(
                  key: _mapKey,
                  initialCenter: animated ??
                      _pickupOf(state) ??
                      _dropoffOf(state) ??
                      _cameraFallback,
                  initialZoom: 15.0,
                  interactionMode: MapInteractionMode.pan,
                  showRecenterFab: true,
                  followUserLocation: cameraTarget != null,
                  recenterTarget: cameraTarget,
                  markers: [
                    if (_pickupOf(state) != null)
                      MapMarkerModel(
                        id: 'pickup',
                        position: _pickupOf(state)!,
                        kind: MapMarkerKind.pickup,
                        label: 'Pickup',
                      ),
                    if (_dropoffOf(state) != null)
                      MapMarkerModel(
                        id: 'dropoff',
                        position: _dropoffOf(state)!,
                        kind: MapMarkerKind.dropoff,
                        label: 'Dropoff',
                      ),
                    if (animated != null)
                      MapMarkerModel(
                        id: 'driver',
                        position: _committedDriverPoint ?? animated,
                        kind: MapMarkerKind.driver,
                        label: 'Driver',
                      ),
                  ],
                  polylines: [
                    if (route.length >= 2)
                      MapPolylineModel(id: 'route', points: route),
                  ],
                  onCameraIdle: (_) {},
                ),
              ),

              // Invisible 800ms tween that animates the driver marker and
              // camera between successive WS fixes (§1.1).
              if (animated != null)
                Positioned(
                  left: 0,
                  top: 0,
                  width: 0,
                  height: 0,
                  child: _driverMarker(animated),
                ),

              // Follow toggle: driver vs rider (plan: "Recenter" FAB toggles).
              if (animated != null)
                Positioned(
                  right: 16,
                  bottom: 340,
                  child: FloatingActionButton.small(
                    heroTag: 'tracking-follow-toggle',
                    tooltip: _followDriver
                        ? 'Following driver — tap to follow me'
                        : 'Following you — tap to follow driver',
                    onPressed: () => setState(() => _followDriver = !_followDriver),
                    backgroundColor: AppColors.surface,
                    foregroundColor: AppColors.primary,
                    child: Icon(_followDriver
                        ? Icons.directions_car
                        : Icons.person_pin_circle),
                  ),
                ),

              // Back Button
              Positioned(
                top: 50,
                left: 16,
                child: Container(
                  decoration: BoxDecoration(
                    color: Colors.white,
                    borderRadius: BorderRadius.circular(12),
                    boxShadow: [
                      BoxShadow(
                        color: Colors.black.withOpacity(0.1),
                        blurRadius: 8,
                      ),
                    ],
                  ),
                  child: IconButton(
                    icon: const Icon(Icons.arrow_back),
                    onPressed: () => _confirmCancel(context, state),
                  ),
                ),
              ),

              // Safety shield — regulator-required entry point, always
              // reachable while a trip is active.
              Positioned(
                top: 50,
                right: 16,
                child: Container(
                  decoration: BoxDecoration(
                    color: AppColors.error.withOpacity(0.1),
                    borderRadius: BorderRadius.circular(12),
                    border: Border.all(color: AppColors.error),
                  ),
                  child: IconButton(
                    icon: const Icon(Icons.shield, color: AppColors.error),
                    tooltip: 'Safety Toolkit',
                    onPressed: () => SafetyToolkitPage.open(context),
                  ),
                ),
              ),

              if (followRider)
                Positioned(
                  top: 100,
                  left: 16,
                  child: _FollowingChip(label: 'Following you'),
                ),

              // Bottom Sheet
              Positioned(
                bottom: 0,
                left: 0,
                right: 0,
                child: Container(
                  padding: const EdgeInsets.all(24),
                  decoration: const BoxDecoration(
                    color: Colors.white,
                    borderRadius: BorderRadius.only(
                      topLeft: Radius.circular(24),
                      topRight: Radius.circular(24),
                    ),
                  ),
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      // Status Indicator
                      _buildStatusIndicator(state),

                      // §3.1 Cash badge — shown only when the backend says
                      // this ride is cash-paid. Three honest states, all
                      // server-sourced: awaiting collection (driver has not
                      // confirmed yet) vs collected at a real timestamp.
                      if (state is RideInProgress && state.ride.isCashRide) ...[
                        const SizedBox(height: 8),
                        _CashRideBadge(ride: state.ride),
                      ],

                      const SizedBox(height: 24),

                      // Driver Card (matched or en route)
                      if (state is DriverMatched) ...[
                        DriverCard(
                          driverName: state.driver.name,
                          rating: state.driver.rating,
                          carModel: state.driver.vehicleType,
                          plateNumber: state.driver.vehiclePlate,
                          eta: state.etaMinutes,
                        ),
                        const SizedBox(height: 16),
                      ] else if (state is DriverEnRoute) ...[
                        DriverCard(
                          driverName: state.driver.name,
                          rating: state.driver.rating,
                          carModel: state.driver.vehicleType,
                          plateNumber: state.driver.vehiclePlate,
                          eta: state.etaMinutes,
                        ),
                        const SizedBox(height: 12),
                        // §2.3 "Verify your driver" — one honest render per
                        // backend state; never a placeholder photo.
                        VerifyDriverCard(
                          preSelfie: state.preSelfie,
                          driverName: state.driver.name,
                          onRecheck: () =>
                              context.read<RideBloc>().add(LoadRide(widget.rideId)),
                          onContactDriver: () => Navigator.of(context).push(
                            MaterialPageRoute(
                              builder: (_) => ChatPage(rideId: widget.rideId),
                            ),
                          ),
                        ),
                        const SizedBox(height: 12),
                        _PinCodeCard(pinCode: state.pinCode),
                        const SizedBox(height: 16),
                      ],

                      // §3.5 "Change destination" — visible only during the
                      // trip itself and only when the ride_stopovers flag is
                      // on (kill-switch semantics: flag off renders nothing,
                      // never a dead button). The backend gate
                      // (CanChangeDestination) is authoritative; this UI
                      // merely mirrors it for the in_progress state.
                      if (state is RideInProgress &&
                          getIt<FeatureFlagService>()
                              .isEnabled('ride_stopovers')) ...[
                        SizedBox(
                          width: double.infinity,
                          child: OutlinedButton.icon(
                            onPressed: _changeDestinationBusy
                                ? null
                                : () => _startChangeDestination(context),
                            icon: _changeDestinationBusy
                                ? const SizedBox(
                                    height: 16,
                                    width: 16,
                                    child: CircularProgressIndicator(
                                      strokeWidth: 2,
                                    ),
                                  )
                                : const Icon(Icons.alt_route),
                            label: Text(_changeDestinationBusy
                                ? 'Updating destination…'
                                : 'Change destination'),
                          ),
                        ),
                        const SizedBox(height: 12),
                      ],

                      // Action Buttons
                      Row(
                        children: [
                          Expanded(
                            child: OutlinedButton.icon(
                              onPressed: () => ShareTripSheet.showInline(
                                context,
                                rideId: widget.rideId,
                              ),
                              icon: const Icon(Icons.share_location),
                              label: const Text('Share'),
                            ),
                          ),
                          const SizedBox(width: 12),
                          Expanded(
                            child: ElevatedButton.icon(
                              style: ElevatedButton.styleFrom(
                                backgroundColor: AppColors.error,
                                foregroundColor: Colors.white,
                              ),
                              onPressed: () => _confirmCancel(context, state),
                              icon: const Icon(Icons.cancel_outlined),
                              label: const Text('Cancel'),
                            ),
                          ),
                        ],
                      ),
                    ],
                  ),
                ),
              ),
            ],
          ),
        );
      },
    );
  }

  // ─── §3.5 Mid-trip destination change ──────────────────────────────────

  final GeocodingService _geocoding = getIt<GeocodingService>();

  /// Guards against double-taps while the PATCH is in flight.
  bool _changeDestinationBusy = false;

  Future<void> _startChangeDestination(BuildContext context) async {
    // 1. Pick the new place through the same §1.3 autocomplete surface used
    //    at request time (provider chain, cache degradation, honest empty
    //    states all reused — no second search implementation).
    final ride = _currentRide(context);
    final bias = ride != null
        ? LatLng(ride.pickupLat, ride.pickupLng)
        : null;
    final outcome = await PlaceAutocompleteSheet.show(
      context,
      geocoding: _geocoding,
      title: 'New destination',
      bias: bias,
    );
    if (!mounted) return;
    if (outcome.kind != PlaceAutocompleteOutcomeKind.suggestion) return;
    final suggestion = outcome.suggestion!;

    // 2. Confirmation sheet BEFORE the PATCH. We cannot know the new fare
    //    without calling the server (the server owns pricing per Global
    //    Rule 2), so the sheet honestly says the fare will be recomputed
    //    and shows where the change lands — never an invented number.
    final address = suggestion.detail.isEmpty
        ? suggestion.name
        : '${suggestion.name}, ${suggestion.detail}';
    final proceed = await showDialog<bool>(
      context: context,
      builder: (dialogCtx) => AlertDialog(
        title: const Text('Change destination?'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('New destination:\n$address'),
            const SizedBox(height: 12),
            const Text(
              'Your fare will be recalculated by Nidus for the new route. '
              'The updated price is shown right after you confirm.',
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(dialogCtx).pop(false),
            child: const Text('Keep current destination'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(dialogCtx).pop(true),
            child: const Text('Change destination'),
          ),
        ],
      ),
    );
    if (proceed != true || !mounted) return;

    setState(() => _changeDestinationBusy = true);
    try {
      final result = await getIt<RideRepository>().changeDestination(
        rideId: widget.rideId,
        dropoffLat: suggestion.location.latitude,
        dropoffLng: suggestion.location.longitude,
        dropoffAddress: address,
      );
      if (!mounted) return;
      setState(() => _changeDestinationBusy = false);

      if (result.isSuccess) {
        // Money lines come from the SERVER response only — previous fare →
        // new fare, exactly what the rider will pay.
        final messenger = ScaffoldMessenger.of(context);
        context.read<RideBloc>().add(LoadRide(widget.rideId));
        messenger.showSnackBar(
          SnackBar(
            content: Text(
              'Destination updated. Fare ${_formatFare(result.previousFare, result.currency)} → '
              '${_formatFare(result.newFare, result.currency)}',
            ),
            behavior: SnackBarBehavior.floating,
          ),
        );
        return;
      }

      // The repository converts transport/auth failures into typed
      // DestinationChangeResult.failure values (never throws), so every
      // outcome below carries a server error code or an honest local one.
      switch (result.errorCode) {
        case 'DESTINATION_NOT_CHANGEABLE':
        case 'INVALID_TRANSITION':
        case 'RIDE_NOT_CHANGEABLE':
          // State race: the ride moved on (completed/cancelled) between the
          // confirmation tap and the PATCH. Re-fetch so the UI reflects the
          // truth and explain — do not retry blindly.
          final messenger = ScaffoldMessenger.of(context);
          context.read<RideBloc>().add(LoadRide(widget.rideId));
          messenger.showSnackBar(
            SnackBar(
              content: Text(
                result.error ??
                    'The ride status changed before the update could apply. '
                        'Destination can no longer be changed.',
              ),
              behavior: SnackBarBehavior.floating,
            ),
          );
          return;
        case 'FORBIDDEN':
        case 'UNAUTHORIZED':
        case 'NOT_FOUND':
          // Ownership or existence problem — surfaced plainly; caller-level
          // navigation handles it, we just inform and stop. No retry loop.
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(
              content: Text(
                result.error ??
                    'This ride can\'t be updated from this device.',
              ),
              behavior: SnackBarBehavior.floating,
            ),
          );
          return;
        default:
          // Everything else (network, 5xx, malformed response): honest
          // retry affordance, never a silent success or a locally-estimated
          // fare.
          final retry = await showDialog<bool>(
            context: context,
            builder: (dialogCtx) => AlertDialog(
              title: const Text('Couldn\'t update destination'),
              content: Text(
                result.error ??
                    'The server did not accept the change. Your current '
                        'destination is unchanged.',
              ),
              actions: [
                TextButton(
                  onPressed: () => Navigator.of(dialogCtx).pop(false),
                  child: const Text('Keep current destination'),
                ),
                FilledButton(
                  onPressed: () => Navigator.of(dialogCtx).pop(true),
                  child: const Text('Retry'),
                ),
              ],
            ),
          );
          if (retry == true && mounted) {
            await _submitChangeDestination(address, suggestion.location);
          }
      }
    } catch (e) {
      // Defensive: the repository contract says it never throws, but a
      // serialization bug must not strand the button in "busy" state.
      if (!mounted) return;
      setState(() => _changeDestinationBusy = false);
      debugPrint('Unexpected changeDestination throw: $e');
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(
          content: Text(
            'Could not reach the server. Your destination was not changed.',
          ),
          behavior: SnackBarBehavior.floating,
        ),
      );
    }
  }

  /// Retry path re-sends the already-confirmed selection (no re-picking).
  Future<void> _submitChangeDestination(
    String address,
    LatLng location,
  ) async {
    setState(() => _changeDestinationBusy = true);
    try {
      final result = await getIt<RideRepository>().changeDestination(
        rideId: widget.rideId,
        dropoffLat: location.latitude,
        dropoffLng: location.longitude,
        dropoffAddress: address,
      );
      if (!mounted) return;
      setState(() => _changeDestinationBusy = false);
      final messenger = ScaffoldMessenger.of(context);
      if (result.isSuccess) {
        context.read<RideBloc>().add(LoadRide(widget.rideId));
        messenger.showSnackBar(
          SnackBar(
            content: Text(
              'Destination updated. Fare ${_formatFare(result.previousFare, result.currency)} → '
              '${_formatFare(result.newFare, result.currency)}',
            ),
            behavior: SnackBarBehavior.floating,
          ),
        );
      } else {
        messenger.showSnackBar(
          SnackBar(
            content: Text(
              result.error ?? 'Destination update failed. Try again later.',
            ),
            behavior: SnackBarBehavior.floating,
          ),
        );
      }
    } catch (_) {
      if (!mounted) return;
      setState(() => _changeDestinationBusy = false);
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(
          content: Text('Still unreachable. Your destination was not changed.'),
          behavior: SnackBarBehavior.floating,
        ),
      );
    }
  }

  /// Best-effort read of the bloc's current ride for map bias; null-safe.
  Ride? _currentRide(BuildContext context) {
    final state = context.read<RideBloc>().state;
    if (state is RideInProgress) return state.ride;
    return null;
  }

  String _formatFare(double? amount, String? currency) {
    if (amount == null) return 'unavailable';
    final unit = currency ?? 'ETB';
    return '$unit ${amount.toStringAsFixed(2)}';
  }

  Future<void> _confirmCancel(BuildContext context, RideState state) async {
    final bloc = context.read<RideBloc>();

    // §2.5: the fee, reason code and free-window countdown are fetched from
    // GET /rides/{id}/cancellation-quote inside the sheet itself — computed
    // by the same backend policy service that charges the fee on cancel. No
    // client-side heuristic exists in this flow anymore.
    await CancelRideSheet.show(
      context,
      rideId: widget.rideId,
      onConfirmed: (reason) async {
        bloc.add(CancelRide(rideId: widget.rideId, reason: reason));
      },
    );
  }

  Widget _buildStatusIndicator(RideState state) {
    String statusText;
    Color statusColor;

    if (state is RideRequested) {
      statusText = 'Finding your driver...';
      statusColor = AppColors.warning;
    } else if (state is DriverEnRoute) {
      statusText = 'Your driver is arriving in ${state.etaMinutes} min';
      statusColor = AppColors.primary;
    } else if (state is DriverMatched) {
      statusText = 'Driver is on the way';
      statusColor = AppColors.success;
    } else if (state is RideInProgress) {
      statusText = 'On trip';
      statusColor = AppColors.success;
    } else if (state is RideCompleted) {
      statusText = 'Trip complete';
      statusColor = AppColors.success;
    } else if (state is RideCancelled) {
      statusText = state.wasDriverCancelled
          ? 'Driver cancelled — no fee charged'
          : 'Ride cancelled';
      statusColor = AppColors.error;
    } else if (state is NoDriversFound) {
      statusText = 'No drivers available nearby';
      statusColor = AppColors.warning;
    } else if (state is PaymentFailed) {
      statusText = 'Payment issue — resolve to continue';
      statusColor = AppColors.error;
    } else {
      statusText = 'Preparing your ride';
      statusColor = AppColors.info;
    }

    return Row(
      children: [
        Container(
          width: 12,
          height: 12,
          decoration: BoxDecoration(
            color: statusColor,
            shape: BoxShape.circle,
          ),
        ),
        const SizedBox(width: 12),
        Expanded(
          child: Text(
            statusText,
            style: TextStyle(
              fontSize: 18,
              fontWeight: FontWeight.w600,
              color: statusColor,
            ),
          ),
        ),
        if (state is RideCompleted)
          TextButton(
            onPressed: () => ReceiptDetailPage.open(
              context,
              ride: state.ride,
            ),
            child: const Text('Receipt'),
          ),
      ],
    );
  }
}

class _FollowingChip extends StatelessWidget {
  final String label;

  const _FollowingChip({required this.label});

  @override
  Widget build(BuildContext context) {
    return Material(
      color: AppColors.primary.withOpacity(0.9),
      borderRadius: BorderRadius.circular(20),
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Icon(Icons.gps_fixed, color: Colors.white, size: 14),
            const SizedBox(width: 6),
            Text(
              label,
              style: const TextStyle(color: Colors.white, fontSize: 12),
            ),
          ],
        ),
      ),
    );
  }
}

/// §3.1 Cash-ride badge for the tracking sheet. Every state is derived from
/// server fields only — `payment_method_type`, `cash_collected_at` — never a
/// fabricated amount or status. When collection is pending the rider sees the
/// honest "collect at drop-off" note; once the driver confirms, the real
/// collected timestamp is rendered.
class _CashRideBadge extends StatelessWidget {
  final Ride ride;

  const _CashRideBadge({required this.ride});

  @override
  Widget build(BuildContext context) {
    final collected = ride.cashCollectedAt;
    final pending = ride.pendingCashCollection ?? collected == null;
    final icon = collected != null
        ? Icons.receipt_long
        : (pending ? Icons.payments_outlined : Icons.hourglass_empty);
    final label = collected != null
        ? 'Cash collected ${_formatCollected(collected)}'
        : 'Cash ride — pay the driver at drop-off';

    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
      decoration: BoxDecoration(
        color: AppColors.accentLight.withValues(alpha: 0.25),
        borderRadius: BorderRadius.circular(10),
        border: Border.all(color: AppColors.accent),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(icon, size: 16, color: AppColors.accentDark),
          const SizedBox(width: 8),
          Flexible(
            child: Text(
              label,
              style: const TextStyle(
                fontSize: 12,
                fontWeight: FontWeight.w600,
                color: AppColors.textPrimary,
              ),
            ),
          ),
        ],
      ),
    );
  }

  String _formatCollected(DateTime value) {
    final local = value.toLocal();
    final h = local.hour.toString().padLeft(2, '0');
    final m = local.minute.toString().padLeft(2, '0');
    return 'at $h:$m';
  }
}

/// Anti-impersonation card: the rider shares this PIN with the driver at
/// pickup; the driver must enter it before the trip can start.
class _PinCodeCard extends StatelessWidget {
  final String pinCode;

  const _PinCodeCard({required this.pinCode});

  @override
  Widget build(BuildContext context) {
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: AppColors.primary.withOpacity(0.08),
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: AppColors.primary),
      ),
      child: Column(
        children: [
          const Text(
            'Share this PIN with your driver',
            style: TextStyle(fontSize: 12, color: AppColors.textSecondary),
          ),
          const SizedBox(height: 8),
          Text(
            pinCode,
            style: const TextStyle(
              fontSize: 32,
              fontWeight: FontWeight.w800,
              letterSpacing: 8,
              color: AppColors.primary,
            ),
          ),
        ],
      ),
    );
  }
}
