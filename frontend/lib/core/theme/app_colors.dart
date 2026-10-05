import 'package:flutter/material.dart';

/// NIDAW Brand Color System
/// 
/// This class defines all colors used throughout the NIDAW platform.
/// Colors are organized by:
/// - Brand colors (primary, secondary)
/// - Service colors (Nidus, Haven, Vorax, Logix, Corpus)
/// - Semantic colors (success, error, warning, info)
/// - Neutral colors (background, surface, text)
class AppColors {
  AppColors._();

  // ============================================================================
  // BRAND COLORS
  // ============================================================================

  /// Primary brand color - Deep Blue
  /// Used for: CTAs, headers, primary actions
  static const Color primary = Color(0xFF1E40AF);
  static const Color primaryLight = Color(0xFF3B82F6);
  static const Color primaryDark = Color(0xFF1E3A8A);

  /// Secondary brand color - Teal
  /// Used for: Success states, fresh elements, accents
  static const Color secondary = Color(0xFF0D9488);
  static const Color secondaryLight = Color(0xFF14B8A6);
  static const Color secondaryDark = Color(0xFF0F766E);

  /// Accent color - Amber
  /// Used for: Highlights, promotions, warnings
  static const Color accent = Color(0xFFF59E0B);
  static const Color accentLight = Color(0xFFFCD34D);
  static const Color accentDark = Color(0xFFD97706);

  // ============================================================================
  // SERVICE COLORS
  // ============================================================================

  /// Nidus (Rides) - Blue
  static const Color nidusBlue = Color(0xFF2563EB);
  static const Color nidusBlueLight = Color(0xFF60A5FA);
  static const Color nidusBlueDark = Color(0xFF1D4ED8);

  /// Haven (Hotels) - Purple
  static const Color havenPurple = Color(0xFF8B5CF6);
  static const Color havenPurpleLight = Color(0xFFA78BFA);
  static const Color havenPurpleDark = Color(0xFF7C3AED);

  /// Vorax (Food) - Orange
  static const Color voraxOrange = Color(0xFFF97316);
  static const Color voraxOrangeLight = Color(0xFFFB923C);
  static const Color voraxOrangeDark = Color(0xFFEA580C);

  /// Logix (Freight) - Green
  static const Color logixGreen = Color(0xFF10B981);
  static const Color logixGreenLight = Color(0xFF34D399);
  static const Color logixGreenDark = Color(0xFF059669);

  /// Corpus (Corporate) - Teal
  static const Color corpusTeal = Color(0xFF14B8A6);
  static const Color corpusTealLight = Color(0xFF2DD4BF);
  static const Color corpusTealDark = Color(0xFF0D9488);

  // ============================================================================
  // SEMANTIC COLORS
  // ============================================================================

  /// Success - Green
  static const Color success = Color(0xFF10B981);
  static const Color successLight = Color(0xFF34D399);
  static const Color successDark = Color(0xFF059669);

  /// Error - Red
  static const Color error = Color(0xFFEF4444);
  static const Color errorLight = Color(0xFFF87171);
  static const Color errorDark = Color(0xFFDC2626);

  /// Warning - Amber
  static const Color warning = Color(0xFFF59E0B);
  static const Color warningLight = Color(0xFFFCD34D);
  static const Color warningDark = Color(0xFFD97706);

  /// Info - Blue
  static const Color info = Color(0xFF3B82F6);
  static const Color infoLight = Color(0xFF60A5FA);
  static const Color infoDark = Color(0xFF2563EB);

  // ============================================================================
  // NEUTRAL COLORS
  // ============================================================================

  /// Background colors
  static const Color background = Color(0xFFF8FAFC);
  static const Color surface = Color(0xFFFFFFFF);
  static const Color surfaceVariant = Color(0xFFF1F5F9);

  /// Text colors
  static const Color textPrimary = Color(0xFF0F172A);
  static const Color textSecondary = Color(0xFF64748B);
  static const Color textTertiary = Color(0xFF94A3B8);
  static const Color textDisabled = Color(0xFFCBD5E1);

  /// Border colors
  static const Color border = Color(0xFFE2E8F0);
  static const Color borderLight = Color(0xFFF1F5F9);
  static const Color borderDark = Color(0xFFCBD5E1);

  /// Input colors
  static const Color inputBackground = Color(0xFFF8FAFC);
  static const Color inputBorder = Color(0xFFE2E8F0);
  static const Color inputFocused = Color(0xFF3B82F6);

  // ============================================================================
  // GRADIENTS
  // ============================================================================

  /// Primary gradient (Blue to Teal)
  static const LinearGradient primaryGradient = LinearGradient(
    begin: Alignment.topLeft,
    end: Alignment.bottomRight,
    colors: [
      Color(0xFF1E40AF), // Primary Blue
      Color(0xFF0D9488), // Secondary Teal
    ],
  );

  /// Service gradients
  static const LinearGradient nidusGradient = LinearGradient(
    begin: Alignment.topLeft,
    end: Alignment.bottomRight,
    colors: [
      Color(0xFF2563EB),
      Color(0xFF3B82F6),
    ],
  );

  static const LinearGradient havenGradient = LinearGradient(
    begin: Alignment.topLeft,
    end: Alignment.bottomRight,
    colors: [
      Color(0xFF8B5CF6),
      Color(0xFFA78BFA),
    ],
  );

  static const LinearGradient voraxGradient = LinearGradient(
    begin: Alignment.topLeft,
    end: Alignment.bottomRight,
    colors: [
      Color(0xFFF97316),
      Color(0xFFFB923C),
    ],
  );

  static const LinearGradient logixGradient = LinearGradient(
    begin: Alignment.topLeft,
    end: Alignment.bottomRight,
    colors: [
      Color(0xFF10B981),
      Color(0xFF34D399),
    ],
  );

  /// Promo gradient (Amber to Red)
  static const LinearGradient promoGradient = LinearGradient(
    begin: Alignment.topLeft,
    end: Alignment.bottomRight,
    colors: [
      Color(0xFFF59E0B),
      Color(0xFFEF4444),
    ],
  );

  // ============================================================================
  // OPACITY HELPERS
  // ============================================================================

  /// Returns a color with the specified opacity
  static Color withOpacity(Color color, double opacity) {
    return color.withOpacity(opacity);
  }

  /// Light variant of a color (10% opacity)
  static Color light(Color color) => color.withOpacity(0.1);

  /// Medium variant of a color (20% opacity)
  static Color medium(Color color) => color.withOpacity(0.2);

  /// Dark variant of a color (80% opacity)
  static Color dark(Color color) => color.withOpacity(0.8);
}