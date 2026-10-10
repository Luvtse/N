import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:latlong2/latlong.dart';

import '../../../../core/map/live_map_widget.dart';
import '../../../../core/theme/app_colors.dart';
import '../bloc/ride_bloc.dart';
import '../widgets/cancel_ride_sheet.dart';
import '../widgets/driver_card.dart';
import '../widgets/payment_method_selector.dart';
import '../widgets/rate_ride_sheet.dart';
import '../widgets/safety_widgets.dart';
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
                        _PinCodeCard(pinCode: state.pinCode),
                        const SizedBox(height: 16),
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

  Future<void> _confirmCancel(BuildContext context, RideState state) async {
    final bloc = context.read<RideBloc>();

    // Free-window heuristic: within the grace period after matching the
    // backend waives the fee; the authoritative amount comes back on the
    // CancelRideResult and lands in the RideCancelled state.
    final inGraceWindow = state is DriverMatched || state is DriverEnRoute;
    final feeEstimate = inGraceWindow ? 0.0 : 25.0;

    await CancelRideSheet.show(
      context,
      estimatedFee: feeEstimate,
      freeWindowMinutesLeft: inGraceWindow ? 5 : null,
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
