import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:go_router/go_router.dart';
import 'package:intl/intl.dart';

import '../../../../core/theme/app_colors.dart';
import '../../../../core/utils/validators.dart';
import '../../../../core/utils/formatters.dart';

class BookingPage extends StatefulWidget {
  final String hotelId;

  const BookingPage({
    super.key,
    required this.hotelId,
  });

  @override
  State<BookingPage> createState() => _BookingPageState();
}

class _BookingPageState extends State<BookingPage> {
  final _formKey = GlobalKey<FormState>();
  final _guestsController = TextEditingController(text: '2');
  final _roomsController = TextEditingController(text: '1');
  final _specialRequestsController = TextEditingController();

  DateTime _checkIn = DateTime.now().add(const Duration(days: 1));
  DateTime _checkOut = DateTime.now().add(const Duration(days: 3));
  
  bool _isLoading = false;

  // Mock hotel data - replace with actual data from repository
  final Map<String, dynamic> _hotel = {
    'name': 'Grand Plaza Hotel',
    'price_per_night': 299.99,
    'currency': 'USD',
    'address': '123 Broadway, New York, NY',
  };

  @override
  void dispose() {
    _guestsController.dispose();
    _roomsController.dispose();
    _specialRequestsController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final nights = _checkOut.difference(_checkIn).inDays;
    final totalPrice = (_hotel['price_per_night'] as double) * nights;

    return Scaffold(
      appBar: AppBar(
        title: const Text('Book Hotel'),
        leading: IconButton(
          icon: const Icon(Icons.arrow_back),
          onPressed: () => context.pop(),
        ),
      ),
      body: SingleChildScrollView(
        padding: const EdgeInsets.all(24),
        child: Form(
          key: _formKey,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              // Hotel Info Card
              _buildHotelInfoCard(),
              const SizedBox(height: 24),

              // Date Selection
              _buildSectionTitle('Stay Dates'),
              const SizedBox(height: 12),
              _buildDateSelector(),
              const SizedBox(height: 24),

              // Guests and Rooms
              _buildSectionTitle('Guests & Rooms'),
              const SizedBox(height: 12),
              _buildGuestsRoomsSelector(),
              const SizedBox(height: 24),

              // Special Requests
              _buildSectionTitle('Special Requests (Optional)'),
              const SizedBox(height: 12),
              _buildSpecialRequestsField(),
              const SizedBox(height: 32),

              // Price Summary
              _buildPriceSummary(nights, totalPrice),
              const SizedBox(height: 32),

              // Book Button
              ElevatedButton(
                onPressed: _isLoading ? null : _bookHotel,
                style: ElevatedButton.styleFrom(
                  padding: const EdgeInsets.symmetric(vertical: 18),
                  backgroundColor: AppColors.primary,
                ),
                child: _isLoading
                    ? const SizedBox(
                        width: 20,
                        height: 20,
                        child: CircularProgressIndicator(
                          strokeWidth: 2,
                          valueColor: AlwaysStoppedAnimation<Color>(Colors.white),
                        ),
                      )
                    : Text(
                        'Book Now - ${Formatters.formatCurrency(totalPrice)}',
                        style: const TextStyle(
                          fontSize: 18,
                          fontWeight: FontWeight.bold,
                        ),
                      ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildHotelInfoCard() {
    return Container(
      padding: const EdgeInsets.all(20),
      decoration: BoxDecoration(
        color: AppColors.primary.withOpacity(0.1),
        borderRadius: BorderRadius.circular(16),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            _hotel['name'] as String,
            style: const TextStyle(
              fontSize: 24,
              fontWeight: FontWeight.bold,
            ),
          ),
          const SizedBox(height: 8),
          Row(
            children: [
              const Icon(Icons.location_on, size: 16, color: AppColors.textSecondary),
              const SizedBox(width: 4),
              Expanded(
                child: Text(
                  _hotel['address'] as String,
                  style: const TextStyle(color: AppColors.textSecondary),
                ),
              ),
            ],
          ),
          const SizedBox(height: 12),
          Row(
            children: [
              const Icon(Icons.star, size: 20, color: Colors.amber),
              const SizedBox(width: 4),
              const Text(
                '4.8',
                style: TextStyle(fontWeight: FontWeight.bold),
              ),
              const SizedBox(width: 16),
              Text(
                '${Formatters.formatCurrency(_hotel['price_per_night'] as double)}/night',
                style: const TextStyle(
                  fontSize: 18,
                  fontWeight: FontWeight.bold,
                  color: AppColors.primary,
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }

  Widget _buildSectionTitle(String title) {
    return Text(
      title,
      style: const TextStyle(
        fontSize: 18,
        fontWeight: FontWeight.bold,
      ),
    );
  }

  Widget _buildDateSelector() {
    return Row(
      children: [
        Expanded(
          child: _buildDateField(
            label: 'Check-in',
            date: _checkIn,
            onTap: () => _selectDate(true),
          ),
        ),
        const SizedBox(width: 12),
        const Icon(Icons.arrow_forward, color: AppColors.textSecondary),
        const SizedBox(width: 12),
        Expanded(
          child: _buildDateField(
            label: 'Check-out',
            date: _checkOut,
            onTap: () => _selectDate(false),
          ),
        ),
      ],
    );
  }

  Widget _buildDateField({
    required String label,
    required DateTime date,
    required VoidCallback onTap,
  }) {
    return InkWell(
      onTap: onTap,
      borderRadius: BorderRadius.circular(12),
      child: Container(
        padding: const EdgeInsets.all(16),
        decoration: BoxDecoration(
          border: Border.all(color: AppColors.border),
          borderRadius: BorderRadius.circular(12),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              label,
              style: const TextStyle(
                fontSize: 12,
                color: AppColors.textSecondary,
              ),
            ),
            const SizedBox(height: 4),
            Text(
              Formatters.formatShortDate(date),
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

  Widget _buildGuestsRoomsSelector() {
    return Row(
      children: [
        Expanded(
          child: TextFormField(
            controller: _guestsController,
            keyboardType: TextInputType.number,
            decoration: const InputDecoration(
              labelText: 'Guests',
              prefixIcon: Icon(Icons.people),
            ),
            validator: (value) {
              if (value == null || value.isEmpty) {
                return 'Required';
              }
              final guests = int.tryParse(value);
              if (guests == null || guests < 1 || guests > 10) {
                return 'Enter 1-10';
              }
              return null;
            },
          ),
        ),
        const SizedBox(width: 12),
        Expanded(
          child: TextFormField(
            controller: _roomsController,
            keyboardType: TextInputType.number,
            decoration: const InputDecoration(
              labelText: 'Rooms',
              prefixIcon: Icon(Icons.hotel),
            ),
            validator: (value) {
              if (value == null || value.isEmpty) {
                return 'Required';
              }
              final rooms = int.tryParse(value);
              if (rooms == null || rooms < 1 || rooms > 5) {
                return 'Enter 1-5';
              }
              return null;
            },
          ),
        ),
      ],
    );
  }

  Widget _buildSpecialRequestsField() {
    return TextFormField(
      controller: _specialRequestsController,
      maxLines: 3,
      decoration: const InputDecoration(
        hintText: 'e.g., Early check-in, extra pillows, etc.',
        border: OutlineInputBorder(),
      ),
    );
  }

  Widget _buildPriceSummary(int nights, double totalPrice) {
    final pricePerNight = _hotel['price_per_night'] as double;

    return Container(
      padding: const EdgeInsets.all(20),
      decoration: BoxDecoration(
        color: Colors.grey[100],
        borderRadius: BorderRadius.circular(16),
      ),
      child: Column(
        children: [
          _buildPriceRow('Price per night', Formatters.formatCurrency(pricePerNight)),
          const SizedBox(height: 8),
          _buildPriceRow('Number of nights', '$nights night${nights > 1 ? 's' : ''}'),
          const Divider(height: 24),
          _buildPriceRow(
            'Total',
            Formatters.formatCurrency(totalPrice),
            isTotal: true,
          ),
        ],
      ),
    );
  }

  Widget _buildPriceRow(String label, String value, {bool isTotal = false}) {
    return Row(
      mainAxisAlignment: MainAxisAlignment.spaceBetween,
      children: [
        Text(
          label,
          style: TextStyle(
            fontSize: isTotal ? 18 : 16,
            fontWeight: isTotal ? FontWeight.bold : FontWeight.normal,
          ),
        ),
        Text(
          value,
          style: TextStyle(
            fontSize: isTotal ? 24 : 16,
            fontWeight: FontWeight.bold,
            color: isTotal ? AppColors.primary : null,
          ),
        ),
      ],
    );
  }

  Future<void> _selectDate(bool isCheckIn) async {
    final picked = await showDatePicker(
      context: context,
      initialDate: isCheckIn ? _checkIn : _checkOut,
      firstDate: DateTime.now(),
      lastDate: DateTime.now().add(const Duration(days: 365)),
    );

    if (picked != null) {
      setState(() {
        if (isCheckIn) {
          _checkIn = picked;
          if (_checkOut.isBefore(_checkIn)) {
            _checkOut = _checkIn.add(const Duration(days: 1));
          }
        } else {
          _checkOut = picked;
        }
      });
    }
  }

  void _bookHotel() {
    if (!_formKey.currentState!.validate()) {
      return;
    }

    setState(() => _isLoading = true);

    // TODO: Implement actual booking logic
    Future.delayed(const Duration(seconds: 2), () {
      setState(() => _isLoading = false);
      
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(
          content: Text('Booking confirmed!'),
          backgroundColor: AppColors.success,
        ),
      );

      context.go('/home');
    });
  }
}