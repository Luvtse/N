import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import 'package:cached_network_image/cached_network_image.dart';

import '../../../../core/theme/app_colors.dart';

class MenuPage extends StatefulWidget {
  final String restaurantId;

  const MenuPage({super.key, required this.restaurantId});

  @override
  State<MenuPage> createState() => _MenuPageState();
}

class _MenuPageState extends State<MenuPage> {
  final Map<String, int> _cart = {};
  String _selectedCategory = 'All';

  final List<Map<String, dynamic>> _menuItems = [
    {
      'id': '1',
      'name': 'Margherita Pizza',
      'description': 'Fresh tomatoes, mozzarella, basil',
      'price': 14.99,
      'category': 'Pizza',
      'image': 'https://images.unsplash.com/photo-1604068549290-dea0e4a305ca',
    },
    {
      'id': '2',
      'name': 'Pepperoni Pizza',
      'description': 'Classic pepperoni with cheese',
      'price': 16.99,
      'category': 'Pizza',
      'image': 'https://images.unsplash.com/photo-1628840042765-356cda07504e',
    },
    {
      'id': '3',
      'name': 'Caesar Salad',
      'description': 'Romaine lettuce, croutons, parmesan',
      'price': 9.99,
      'category': 'Salads',
      'image': 'https://images.unsplash.com/photo-1546793665-c74683f339c1',
    },
    {
      'id': '4',
      'name': 'Tiramisu',
      'description': 'Classic Italian dessert',
      'price': 7.99,
      'category': 'Desserts',
      'image': 'https://images.unsplash.com/photo-1571877227200-a0d98ea607e9',
    },
  ];

  @override
  Widget build(BuildContext context) {
    final categories = ['All', ..._menuItems.map((item) => item['category'] as String).toSet()];
    final filteredItems = _selectedCategory == 'All'
        ? _menuItems
        : _menuItems.where((item) => item['category'] == _selectedCategory).toList();

    return Scaffold(
      backgroundColor: AppColors.background,
      appBar: AppBar(
        title: const Text('Menu'),
        backgroundColor: Colors.white,
        elevation: 0,
        leading: IconButton(
          icon: const Icon(Icons.arrow_back),
          onPressed: () => context.pop(),
        ),
        actions: [
          Stack(
            children: [
              IconButton(
                icon: const Icon(Icons.shopping_cart),
                onPressed: () => _showCart(),
              ),
              if (_cart.isNotEmpty)
                Positioned(
                  right: 8,
                  top: 8,
                  child: Container(
                    padding: const EdgeInsets.all(4),
                    decoration: const BoxDecoration(
                      color: AppColors.voraxOrange,
                      shape: BoxShape.circle,
                    ),
                    child: Text(
                      _cart.values.fold(0, (sum, count) => sum + count).toString(),
                      style: const TextStyle(
                        color: Colors.white,
                        fontSize: 10,
                        fontWeight: FontWeight.bold,
                      ),
                    ),
                  ),
                ),
            ],
          ),
        ],
      ),
      body: Column(
        children: [
          // Category filter
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
            color: Colors.white,
            child: SingleChildScrollView(
              scrollDirection: Axis.horizontal,
              child: Row(
                children: categories.map((category) {
                  final isSelected = _selectedCategory == category;
                  return Padding(
                    padding: const EdgeInsets.only(right: 8),
                    child: ChoiceChip(
                      label: Text(category),
                      selected: isSelected,
                      onSelected: (selected) {
                        setState(() => _selectedCategory = category);
                      },
                      selectedColor: AppColors.voraxOrange,
                      labelStyle: TextStyle(
                        color: isSelected ? Colors.white : AppColors.textPrimary,
                      ),
                    ),
                  );
                }).toList(),
              ),
            ),
          ),

          // Menu items
          Expanded(
            child: ListView.builder(
              padding: const EdgeInsets.all(16),
              itemCount: filteredItems.length,
              itemBuilder: (context, index) {
                final item = filteredItems[index];
                return _buildMenuItem(item);
              },
            ),
          ),
        ],
      ),
      bottomNavigationBar: _cart.isNotEmpty ? _buildCartBar() : null,
    );
  }

  Widget _buildMenuItem(Map<String, dynamic> item) {
    final quantity = _cart[item['id']] ?? 0;

    return Card(
      margin: const EdgeInsets.only(bottom: 12),
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Row(
          children: [
            ClipRRect(
              borderRadius: BorderRadius.circular(12),
              child: CachedNetworkImage(
                imageUrl: item['image'] as String,
                width: 80,
                height: 80,
                fit: BoxFit.cover,
              ),
            ),
            const SizedBox(width: 12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    item['name'] as String,
                    style: const TextStyle(
                      fontSize: 16,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                  const SizedBox(height: 4),
                  Text(
                    item['description'] as String,
                    style: TextStyle(
                      fontSize: 12,
                      color: Colors.grey[600],
                    ),
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                  ),
                  const SizedBox(height: 8),
                  Text(
                    '\$${(item['price'] as double).toStringAsFixed(2)}',
                    style: const TextStyle(
                      fontSize: 18,
                      fontWeight: FontWeight.bold,
                      color: AppColors.voraxOrange,
                    ),
                  ),
                ],
              ),
            ),
            if (quantity > 0)
              Column(
                children: [
                  Container(
                    padding: const EdgeInsets.all(8),
                    decoration: BoxDecoration(
                      color: AppColors.voraxOrange,
                      borderRadius: BorderRadius.circular(8),
                    ),
                    child: Text(
                      quantity.toString(),
                      style: const TextStyle(
                        color: Colors.white,
                        fontWeight: FontWeight.bold,
                      ),
                    ),
                  ),
                  const SizedBox(height: 8),
                  TextButton(
                    onPressed: () {
                      setState(() {
                        _cart.remove(item['id']);
                      });
                    },
                    child: const Text('Remove'),
                  ),
                ],
              )
            else
              IconButton(
                icon: const Icon(Icons.add_circle, color: AppColors.voraxOrange),
                onPressed: () {
                  setState(() {
                    _cart[item['id']] = 1;
                  });
                },
              ),
          ],
        ),
      ),
    );
  }

  Widget _buildCartBar() {
    final total = _cart.entries.fold(0.0, (sum, entry) {
      final item = _menuItems.firstWhere((i) => i['id'] == entry.key);
      return sum + (item['price'] as double) * entry.value;
    });

    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: Colors.white,
        boxShadow: [
          BoxShadow(
            color: Colors.black.withOpacity(0.05),
            blurRadius: 10,
            offset: const Offset(0, -2),
          ),
        ],
      ),
      child: Row(
        children: [
          Expanded(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const Text(
                  'Cart Total',
                  style: TextStyle(fontSize: 12, color: AppColors.textSecondary),
                ),
                Text(
                  '\$${total.toStringAsFixed(2)}',
                  style: const TextStyle(
                    fontSize: 24,
                    fontWeight: FontWeight.bold,
                    color: AppColors.voraxOrange,
                  ),
                ),
              ],
            ),
          ),
          ElevatedButton.icon(
            onPressed: () => context.push('/vorax/order/${widget.restaurantId}'),
            icon: const Icon(Icons.shopping_cart),
            label: const Text('Checkout'),
            style: ElevatedButton.styleFrom(
              backgroundColor: AppColors.voraxOrange,
              padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 16),
            ),
          ),
        ],
      ),
    );
  }

  void _showCart() {
    showModalBottomSheet(
      context: context,
      builder: (context) => Container(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Text(
              'Your Cart',
              style: TextStyle(fontSize: 20, fontWeight: FontWeight.bold),
            ),
            const SizedBox(height: 16),
            ..._cart.entries.map((entry) {
              final item = _menuItems.firstWhere((i) => i['id'] == entry.key);
              return ListTile(
                title: Text(item['name'] as String),
                subtitle: Text('x${entry.value}'),
                trailing: Text('\$${((item['price'] as double) * entry.value).toStringAsFixed(2)}'),
              );
            }),
          ],
        ),
      ),
    );
  }
}