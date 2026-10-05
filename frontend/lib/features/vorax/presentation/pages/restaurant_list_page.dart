import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:go_router/go_router.dart';
import '../../../../core/theme/app_colors.dart';
import '../bloc/restaurant_bloc.dart';
import '../widgets/restaurant_card.dart';

class RestaurantListPage extends StatefulWidget {
  const RestaurantListPage({super.key});

  @override
  State<RestaurantListPage> createState() => _RestaurantListPageState();
}

class _RestaurantListPageState extends State<RestaurantListPage> {
  final _searchController = TextEditingController();
  String _selectedCategory = 'All';

  final List<String> _categories = [
    'All', 'Pizza', 'Sushi', 'Burgers', 'Indian', 'Chinese', 'Mexican', 'Desserts'
  ];

  @override
  void dispose() {
    _searchController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Food Delivery'),
      ),
      body: BlocBuilder<RestaurantBloc, RestaurantState>(
        builder: (context, state) {
          return Column(
            children: [
              // Search Bar
              Padding(
                padding: const EdgeInsets.all(16),
                child: TextField(
                  controller: _searchController,
                  decoration: InputDecoration(
                    hintText: 'Search restaurants or dishes',
                    prefixIcon: const Icon(Icons.search),
                    suffixIcon: IconButton(
                      icon: const Icon(Icons.filter_list),
                      onPressed: () {},
                    ),
                  ),
                  onChanged: (value) {
                    context.read<RestaurantBloc>().add(SearchRestaurants(value));
                  },
                ),
              ),

              // Categories
              SizedBox(
                height: 50,
                child: ListView.builder(
                  scrollDirection: Axis.horizontal,
                  padding: const EdgeInsets.symmetric(horizontal: 16),
                  itemCount: _categories.length,
                  itemBuilder: (context, index) {
                    final category = _categories[index];
                    final isSelected = category == _selectedCategory;
                    return Padding(
                      padding: const EdgeInsets.only(right: 8),
                      child: ChoiceChip(
                        label: Text(category),
                        selected: isSelected,
                        onSelected: (selected) {
                          setState(() => _selectedCategory = category);
                          context.read<RestaurantBloc>().add(
                                FilterByCategory(category),
                              );
                        },
                        selectedColor: AppColors.primary,
                        labelStyle: TextStyle(
                          color: isSelected ? Colors.white : Colors.black,
                        ),
                      ),
                    );
                  },
                ),
              ),

              const SizedBox(height: 16),

              // Restaurant List
              Expanded(
                child: state is RestaurantLoaded
                    ? ListView.builder(
                        padding: const EdgeInsets.all(16),
                        itemCount: state.restaurants.length,
                        itemBuilder: (context, index) {
                          final restaurant = state.restaurants[index];
                          return RestaurantCard(
                            restaurant: restaurant,
                            onTap: () => context.push(
                              '/vorax/restaurant/${restaurant.id}',
                            ),
                          );
                        },
                      )
                    : state is RestaurantLoading
                        ? const Center(child: CircularProgressIndicator())
                        : const Center(child: Text('No restaurants found')),
              ),
            ],
          );
        },
      ),
    );
  }
}