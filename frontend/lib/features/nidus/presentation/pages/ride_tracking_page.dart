import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';

import '../../../../core/theme/app_colors.dart';
import '../bloc/ride_bloc.dart';
import '../widgets/cancel_ride_sheet.dart';
import '../widgets/driver_card.dart';
import '../widgets/map_widget.dart';
import '../widgets/payment_method_selector.dart';
import '../widgets/rate_ride_sheet.dart';
import '../widgets/safety_widgets.dart';
import 'receipt_detail_page.dart';
import 'safety_toolkit_page.dart';

class RideTrackingPage extends StatelessWidget {
  final String rideId;

  const RideTrackingPage({super.key, required this.rideId});

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
        return Scaffold(
          body: Stack(
            children: [
              // Map
              MapWidget(
                showUserLocation: true,
                showDriverLocation:
                    state is DriverMatched || state is DriverEnRoute,
                onMapCreated: () {},
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
                                rideId: rideId,
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
        bloc.add(CancelRide(rideId: rideId, reason: reason));
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
