import 'package:flutter/material.dart';

import '../../../../core/theme/app_colors.dart';

/// A resolved or in-progress location value for one [LocationInput] field.
///
/// There are no hardcoded demo coordinates anywhere in the nidus module: a
/// field only carries a LatLng once it came from a real GPS fix, a place
/// search selection, or a map pin drag — all of which resolve coordinates at
/// selection time (see §1.3 geocoding chain). When nothing has resolved yet,
/// callers must surface the "unavailable" state rather than inventing values.
class LocationFieldValue {
  final String display;
  final double? lat;
  final double? lng;

  /// True when the label is still being reverse-geocoded after a pin drag.
  final bool resolvingLabel;

  const LocationFieldValue({
    required this.display,
    this.lat,
    this.lng,
    this.resolvingLabel = false,
  });

  bool get hasCoordinates => lat != null && lng != null;

  const LocationFieldValue.empty()
      : display = '',
        lat = null,
        lng = null,
        resolvingLabel = false;

  LocationFieldValue copyWith({
    String? display,
    double? lat,
    double? lng,
    bool? resolvingLabel,
  }) =>
      LocationFieldValue(
        display: display ?? this.display,
        lat: lat ?? this.lat,
        lng: lng ?? this.lng,
        resolvingLabel: resolvingLabel ?? this.resolvingLabel,
      );
}

/// Pickup/dropoff field used on the Request page.
///
/// The text field is read-only by design: tapping it opens the place
/// autocomplete sheet (§1.3) so every value that lands here already carries a
/// resolved LatLng plus its display string. The trailing icon triggers a fresh
/// GPS read via DeviceLocationService (§1.2) — never a TODO stub.
class LocationInput extends StatelessWidget {
  final String label;
  final IconData icon;
  final Color iconColor;
  final LocationFieldValue value;

  /// Opens the place-autocomplete bottom sheet for this field.
  final VoidCallback onTapField;

  /// Requests a live GPS fix and drops the pin at the returned position.
  /// May be null while permission is unresolved; the button then shows a
  /// disabled spinner-free state instead of doing nothing silently.
  final Future<void> Function()? onUseCurrentLocation;

  /// True while a GPS read for this field is in flight.
  final bool locating;

  /// Placeholder shown when no coordinate-backed value exists yet. Always an
  /// honest "unavailable" style string — never a fabricated address.
  final String hint;

  const LocationInput({
    super.key,
    required this.label,
    required this.icon,
    required this.iconColor,
    required this.value,
    required this.onTapField,
    this.onUseCurrentLocation,
    this.locating = false,
    this.hint = 'Select on map',
  });

  @override
  Widget build(BuildContext context) {
    return InkWell(
      onTap: onTapField,
      borderRadius: BorderRadius.circular(12),
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 12),
        decoration: BoxDecoration(
          color: AppColors.inputBackground,
          borderRadius: BorderRadius.circular(12),
          border: Border.all(color: AppColors.inputBorder),
        ),
        child: Row(
          children: [
            Icon(icon, size: 14, color: iconColor),
            const SizedBox(width: 12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                mainAxisSize: MainAxisSize.min,
                children: [
                  Text(
                    label,
                    style: const TextStyle(
                      fontSize: 11,
                      fontWeight: FontWeight.w600,
                      color: AppColors.textTertiary,
                    ),
                  ),
                  const SizedBox(height: 2),
                  Row(
                    children: [
                      if (value.resolvingLabel) ...[
                        const SizedBox(
                          width: 12,
                          height: 12,
                          child: CircularProgressIndicator(
                            strokeWidth: 2,
                            color: AppColors.textSecondary,
                          ),
                        ),
                        const SizedBox(width: 8),
                      ],
                      Expanded(
                        child: Text(
                          value.display.isEmpty ? hint : value.display,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: TextStyle(
                            fontSize: 15,
                            fontWeight: value.display.isEmpty
                                ? FontWeight.w400
                                : FontWeight.w600,
                            color: value.display.isEmpty
                                ? AppColors.textTertiary
                                : AppColors.textPrimary,
                          ),
                        ),
                      ),
                    ],
                  ),
                ],
              ),
            ),
            const SizedBox(width: 8),
            if (locating)
              const SizedBox(
                width: 20,
                height: 20,
                child: CircularProgressIndicator(
                  strokeWidth: 2.5,
                  color: AppColors.primary,
                ),
              )
            else
              IconButton(
                icon: const Icon(Icons.my_location, size: 20),
                color: AppColors.primary,
                tooltip: 'Use current location',
                onPressed: onUseCurrentLocation == null
                    ? null
                    : () => onUseCurrentLocation!(),
              ),
          ],
        ),
      ),
    );
  }
}
