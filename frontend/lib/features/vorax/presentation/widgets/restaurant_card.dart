import 'package:flutter/material.dart';
import 'package:cached_network_image/cached_network_image.dart';
import '../../../../core/theme/app_colors.dart';

class RestaurantSummary {
  final String id;
  final String name;
  final String cuisineType;
  final double rating;
  final double deliveryFee;
  final int estimatedDeliveryMinutes;
  final String imageUrl;
  final double distanceKm;

  const RestaurantSummary({
    required this.id,
    required this.name,
    required this.cuisineType,
    required this.rating,
    required this.deliveryFee,
    required this.estimatedDeliveryMinutes,
    required this.imageUrl,
    required this.distanceKm,
  });
}

class RestaurantCard extends StatelessWidget {
  final RestaurantSummary restaurant;
  final VoidCallback onTap;

  const RestaurantCard({
    super.key,
    required this.restaurant,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    return Card(
      margin: const EdgeInsets.only(bottom: 16),
      child: InkWell(
        onTap: onTap,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            // Image
            Stack(
              children: [
                ClipRRect(
                  borderRadius: const BorderRadius.vertical(top: Radius.circular(12)),
                  child: CachedNetworkImage(
                    imageUrl: restaurant.imageUrl,
                    height: 150,
                    width: double.infinity,
                    fit: BoxFit.cover,
                    placeholder: (context, url) => Container(
                      color: Colors.grey[300],
                      child: const Center(child: CircularProgressIndicator()),
                    ),
                  ),
                ),
                Positioned(
                  top: 12,
                  right: 12,
                  child: Container(
                    padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                    decoration: BoxDecoration(
                      color: Colors.white,
                      borderRadius: BorderRadius.circular(8),
                    ),
                    child: Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        const Icon(Icons.star, size: 14, color: Colors.amber),
                        const SizedBox(width: 4),
                        Text(
                          restaurant.rating.toString(),
                          style: const TextStyle(fontWeight: FontWeight.bold),
                        ),
                      ],
                    ),
                  ),
                ),
              ],
            ),

            Padding(
              padding: const EdgeInsets.all(16),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  // Name
                  Text(
                    restaurant.name,
                    style: const TextStyle(
                      fontSize: 18,
                      fontWeight: FontWeight.bold,
                    ),
                  ),

                  const SizedBox(height: 4),

                  // Cuisine
                  Text(
                    restaurant.cuisineType,
                    style: TextStyle(color: Colors.grey[600]),
                  ),

                  const SizedBox(height: 12),

                  // Delivery Info
                  Row(
                    children: [
                      _buildInfoChip(
                        icon: Icons.delivery_dining,
                        label: '${restaurant.estimatedDeliveryMinutes} min',
                      ),
                      const SizedBox(width: 12),
                      _buildInfoChip(
                        icon: Icons.attach_money,
                        label: '\$${restaurant.deliveryFee.toStringAsFixed(2)}',
                      ),
                      const SizedBox(width: 12),
                      _buildInfoChip(
                        icon: Icons.location_on,
                        label: '${restaurant.distanceKm.toStringAsFixed(1)} km',
                      ),
                    ],
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildInfoChip({
    required IconData icon,
    required String label,
  }) {
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Icon(icon, size: 16, color: AppColors.primary),
        const SizedBox(width: 4),
        Text(
          label,
          style: const TextStyle(fontSize: 12),
        ),
      ],
    );
  }
}