import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import '../../../../core/theme/app_colors.dart';
import '../bloc/ride_bloc.dart';
import 'map_widget.dart';
import '../widgets/driver_card.dart';

class RideTrackingPage extends StatelessWidget {
  final String rideId;

  const RideTrackingPage({super.key, required this.rideId});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: BlocBuilder<RideBloc, RideState>(
        builder: (context, state) {
          return Stack(
            children: [
              // Map
              MapWidget(
                showUserLocation: true,
                showDriverLocation: state is DriverMatched,
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
                    onPressed: () => Navigator.pop(context),
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
                      
                      // Driver Card (if matched)
                      if (state is DriverMatched) ...[
                        DriverCard(
                          driverName: 'Michael Johnson',
                          rating: 4.9,
                          carModel: 'Toyota Camry',
                          plateNumber: 'ABC 123',
                          eta: state.eta.toInt(),
                        ),
                        const SizedBox(height: 16),
                      ],
                      
                      // Action Buttons
                      Row(
                        children: [
                          Expanded(
                            child: OutlinedButton.icon(
                              onPressed: () {},
                              icon: const Icon(Icons.call),
                              label: const Text('Call'),
                            ),
                          ),
                          const SizedBox(width: 12),
                          Expanded(
                            child: ElevatedButton.icon(
                              onPressed: () {},
                              icon: const Icon(Icons.message),
                              label: const Text('Message'),
                            ),
                          ),
                        ],
                      ),
                    ],
                  ),
                ),
              ),
            ],
          );
        },
      ),
    );
  }

  Widget _buildStatusIndicator(RideState state) {
    String statusText;
    Color statusColor;
    
    if (state is RideRequested) {
      statusText = 'Finding your driver...';
      statusColor = AppColors.warning;
    } else if (state is DriverMatched) {
      statusText = 'Driver is on the way';
      statusColor = AppColors.success;
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
        Text(
          statusText,
          style: TextStyle(
            fontSize: 18,
            fontWeight: FontWeight.w600,
            color: statusColor,
          ),
        ),
      ],
    );
  }
}