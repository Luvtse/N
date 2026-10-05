import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:go_router/go_router.dart';
import '../../../../core/theme/app_colors.dart';
import '../bloc/hotel_search_bloc.dart';
import '../widgets/hotel_card.dart';

class HotelSearchPage extends StatefulWidget {
  const HotelSearchPage({super.key});

  @override
  State<HotelSearchPage> createState() => _HotelSearchPageState();
}

class _HotelSearchPageState extends State<HotelSearchPage> {
  final _cityController = TextEditingController();
  DateTime _checkIn = DateTime.now();
  DateTime _checkOut = DateTime.now().add(const Duration(days: 1));
  int _guests = 2;

  @override
  void dispose() {
    _cityController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Find Hotels'),
      ),
      body: BlocBuilder<HotelSearchBloc, HotelSearchState>(
        builder: (context, state) {
          return Column(
            children: [
              // Search Form
              Container(
                padding: const EdgeInsets.all(20),
                color: Colors.white,
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    // City
                    TextField(
                      controller: _cityController,
                      decoration: const InputDecoration(
                        labelText: 'City',
                        hintText: 'Where are you going?',
                        prefixIcon: Icon(Icons.location_city),
                      ),
                    ),
                    const SizedBox(height: 16),

                    // Dates
                    Row(
                      children: [
                        Expanded(
                          child: _buildDatePicker(
                            label: 'Check-in',
                            date: _checkIn,
                            onSelected: (date) => setState(() => _checkIn = date),
                          ),
                        ),
                        const SizedBox(width: 12),
                        Expanded(
                          child: _buildDatePicker(
                            label: 'Check-out',
                            date: _checkOut,
                            onSelected: (date) => setState(() => _checkOut = date),
                          ),
                        ),
                      ],
                    ),
                    const SizedBox(height: 16),

                    // Guests
                    Row(
                      children: [
                        const Icon(Icons.people, color: AppColors.primary),
                        const SizedBox(width: 12),
                        const Text('Guests:'),
                        const Spacer(),
                        IconButton(
                          icon: const Icon(Icons.remove_circle_outline),
                          onPressed: _guests > 1
                              ? () => setState(() => _guests--)
                              : null,
                        ),
                        Text(
                          '$_guests',
                          style: const TextStyle(
                            fontSize: 18,
                            fontWeight: FontWeight.bold,
                          ),
                        ),
                        IconButton(
                          icon: const Icon(Icons.add_circle_outline),
                          onPressed: () => setState(() => _guests++),
                        ),
                      ],
                    ),
                    const SizedBox(height: 20),

                    // Search Button
                    SizedBox(
                      width: double.infinity,
                      child: ElevatedButton.icon(
                        onPressed: () {
                          context.read<HotelSearchBloc>().add(
                                SearchHotels(
                                  city: _cityController.text,
                                  checkIn: _checkIn,
                                  checkOut: _checkOut,
                                  guests: _guests,
                                ),
                              );
                        },
                        icon: const Icon(Icons.search),
                        label: const Text('Search Hotels'),
                      ),
                    ),
                  ],
                ),
              ),

              // Results
              Expanded(
                child: state is HotelSearchLoaded
                    ? ListView.builder(
                        padding: const EdgeInsets.all(16),
                        itemCount: state.hotels.length,
                        itemBuilder: (context, index) {
                          final hotel = state.hotels[index];
                          return HotelCard(
                            hotel: hotel,
                            onTap: () => context.push('/haven/hotel/${hotel.id}'),
                          );
                        },
                      )
                    : state is HotelSearchLoading
                        ? const Center(child: CircularProgressIndicator())
                        : const Center(child: Text('Search for hotels')),
              ),
            ],
          );
        },
      ),
    );
  }

  Widget _buildDatePicker({
    required String label,
    required DateTime date,
    required ValueChanged<DateTime> onSelected,
  }) {
    return InkWell(
      onTap: () async {
        final picked = await showDatePicker(
          context: context,
          initialDate: date,
          firstDate: DateTime.now(),
          lastDate: DateTime.now().add(const Duration(days: 365)),
        );
        if (picked != null) onSelected(picked);
      },
      child: Container(
        padding: const EdgeInsets.all(12),
        decoration: BoxDecoration(
          border: Border.all(color: Colors.grey[300]!),
          borderRadius: BorderRadius.circular(8),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              label,
              style: TextStyle(fontSize: 12, color: Colors.grey[600]),
            ),
            const SizedBox(height: 4),
            Text(
              '${date.day}/${date.month}/${date.year}',
              style: const TextStyle(fontWeight: FontWeight.w600),
            ),
          ],
        ),
      ),
    );
  }
}