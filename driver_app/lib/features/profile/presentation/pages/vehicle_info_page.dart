import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';

import '../../../../core/theme/app_colors.dart';
import '../bloc/driver_profile_bloc.dart';

class VehicleInfoPage extends StatefulWidget {
  const VehicleInfoPage({super.key});

  @override
  State<VehicleInfoPage> createState() => _VehicleInfoPageState();
}

class _VehicleInfoPageState extends State<VehicleInfoPage> {
  final _formKey = GlobalKey<FormState>();
  final _vehicleTypeController = TextEditingController();
  final _vehicleModelController = TextEditingController();
  final _vehiclePlateController = TextEditingController();
  final _vehicleColorController = TextEditingController();
  final _vehicleYearController = TextEditingController();

  @override
  void dispose() {
    _vehicleTypeController.dispose();
    _vehicleModelController.dispose();
    _vehiclePlateController.dispose();
    _vehicleColorController.dispose();
    _vehicleYearController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Vehicle Information'),
      ),
      body: BlocConsumer<DriverProfileBloc, DriverProfileState>(
        listener: (context, state) {
          if (state is DriverProfileUpdated) {
            ScaffoldMessenger.of(context).showSnackBar(
              SnackBar(
                content: Text(state.message),
                backgroundColor: AppColors.success,
              ),
            );
          } else if (state is DriverProfileError) {
            ScaffoldMessenger.of(context).showSnackBar(
              SnackBar(
                content: Text(state.message),
                backgroundColor: AppColors.error,
              ),
            );
          }
        },
        builder: (context, state) {
          final isLoading = state is DriverProfileLoading;

          return SingleChildScrollView(
            padding: const EdgeInsets.all(24),
            child: Form(
              key: _formKey,
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  const Icon(
                    Icons.directions_car,
                    size: 80,
                    color: AppColors.primary,
                  ),
                  const SizedBox(height: 24),
                  const Text(
                    'Vehicle Details',
                    style: TextStyle(
                      fontSize: 24,
                      fontWeight: FontWeight.bold,
                    ),
                    textAlign: TextAlign.center,
                  ),
                  const SizedBox(height: 32),

                  // Vehicle Type
                  TextFormField(
                    controller: _vehicleTypeController,
                    decoration: const InputDecoration(
                      labelText: 'Vehicle Type',
                      hintText: 'e.g., Sedan, SUV, Van',
                      prefixIcon: Icon(Icons.car_rental),
                    ),
                    validator: (value) {
                      if (value == null || value.isEmpty) {
                        return 'Vehicle type is required';
                      }
                      return null;
                    },
                  ),
                  const SizedBox(height: 16),

                  // Vehicle Model
                  TextFormField(
                    controller: _vehicleModelController,
                    decoration: const InputDecoration(
                      labelText: 'Vehicle Model',
                      hintText: 'e.g., Toyota Camry',
                      prefixIcon: Icon(Icons.directions_car),
                    ),
                    validator: (value) {
                      if (value == null || value.isEmpty) {
                        return 'Vehicle model is required';
                      }
                      return null;
                    },
                  ),
                  const SizedBox(height: 16),

                  // Vehicle Plate
                  TextFormField(
                    controller: _vehiclePlateController,
                    decoration: const InputDecoration(
                      labelText: 'License Plate',
                      hintText: 'e.g., ABC123',
                      prefixIcon: Icon(Icons.confirmation_number),
                    ),
                    validator: (value) {
                      if (value == null || value.isEmpty) {
                        return 'License plate is required';
                      }
                      return null;
                    },
                  ),
                  const SizedBox(height: 16),

                  // Vehicle Color
                  TextFormField(
                    controller: _vehicleColorController,
                    decoration: const InputDecoration(
                      labelText: 'Vehicle Color',
                      hintText: 'e.g., Black, White, Silver',
                      prefixIcon: Icon(Icons.color_lens),
                    ),
                    validator: (value) {
                      if (value == null || value.isEmpty) {
                        return 'Vehicle color is required';
                      }
                      return null;
                    },
                  ),
                  const SizedBox(height: 16),

                  // Vehicle Year
                  TextFormField(
                    controller: _vehicleYearController,
                    keyboardType: TextInputType.number,
                    decoration: const InputDecoration(
                      labelText: 'Vehicle Year',
                      hintText: 'e.g., 2022',
                      prefixIcon: Icon(Icons.calendar_today),
                    ),
                    validator: (value) {
                      if (value == null || value.isEmpty) {
                        return 'Vehicle year is required';
                      }
                      final year = int.tryParse(value);
                      if (year == null || year < 2000 || year > DateTime.now().year) {
                        return 'Enter a valid year';
                      }
                      return null;
                    },
                  ),
                  const SizedBox(height: 32),

                  // Save Button
                  ElevatedButton(
                    onPressed: isLoading ? null : _saveVehicleInfo,
                    style: ElevatedButton.styleFrom(
                      padding: const EdgeInsets.symmetric(vertical: 16),
                    ),
                    child: isLoading
                        ? const SizedBox(
                            width: 20,
                            height: 20,
                            child: CircularProgressIndicator(
                              strokeWidth: 2,
                              valueColor: AlwaysStoppedAnimation<Color>(
                                Colors.white,
                              ),
                            ),
                          )
                        : const Text(
                            'Save Vehicle Information',
                            style: TextStyle(fontSize: 16),
                          ),
                  ),
                ],
              ),
            ),
          );
        },
      ),
    );
  }

  void _saveVehicleInfo() {
    if (!_formKey.currentState!.validate()) {
      return;
    }

    context.read<DriverProfileBloc>().add(
          UpdateVehicleInfo(
            data: {
              'vehicle_type': _vehicleTypeController.text.trim(),
              'vehicle_model': _vehicleModelController.text.trim(),
              'vehicle_plate': _vehiclePlateController.text.trim(),
              'vehicle_color': _vehicleColorController.text.trim(),
              'vehicle_year': int.parse(_vehicleYearController.text.trim()),
            },
          ),
        );
  }
}