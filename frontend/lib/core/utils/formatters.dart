import 'package:intl/intl.dart';

/// Data formatting utilities for NIDAW
class Formatters {
  Formatters._();

  // ============================================================================
  // CURRENCY FORMATTING
  // ============================================================================

  /// Format amount as currency
  static String formatCurrency(double amount, {String currency = 'USD'}) {
    final format = NumberFormat.currency(
      symbol: _getCurrencySymbol(currency),
      decimalDigits: 2,
    );
    return format.format(amount);
  }

  /// Format amount as compact currency (e.g., $1.2K)
  static String formatCompactCurrency(double amount, {String currency = 'USD'}) {
    final format = NumberFormat.compactCurrency(
      symbol: _getCurrencySymbol(currency),
      decimalDigits: 1,
    );
    return format.format(amount);
  }

  /// Get currency symbol
  static String _getCurrencySymbol(String currency) {
    final symbols = {
      'USD': '\$',
      'EUR': '€',
      'GBP': '£',
      'JPY': '¥',
      'CAD': 'C\$',
      'AUD': 'A\$',
    };
    return symbols[currency] ?? currency;
  }

  // ============================================================================
  // DATE FORMATTING
  // ============================================================================

  /// Format date as readable string (e.g., "July 27, 2026")
  static String formatDate(DateTime date) {
    return DateFormat('MMMM d, yyyy').format(date);
  }

  /// Format date as short string (e.g., "Jul 27")
  static String formatShortDate(DateTime date) {
    return DateFormat('MMM d').format(date);
  }

  /// Format date with time (e.g., "July 27, 2026 at 2:30 PM")
  static String formatDateTime(DateTime date) {
    return DateFormat('MMMM d, yyyy \'at\' h:mm a').format(date);
  }

  /// Format time only (e.g., "2:30 PM")
  static String formatTime(DateTime date) {
    return DateFormat('h:mm a').format(date);
  }

  /// Format relative time (e.g., "2 hours ago")
  static String formatRelativeTime(DateTime date) {
    final now = DateTime.now();
    final difference = now.difference(date);

    if (difference.inDays > 365) {
      return '${(difference.inDays / 365).floor()} year${(difference.inDays / 365).floor() > 1 ? 's' : ''} ago';
    } else if (difference.inDays > 30) {
      return '${(difference.inDays / 30).floor()} month${(difference.inDays / 30).floor() > 1 ? 's' : ''} ago';
    } else if (difference.inDays > 0) {
      return '${difference.inDays} day${difference.inDays > 1 ? 's' : ''} ago';
    } else if (difference.inHours > 0) {
      return '${difference.inHours} hour${difference.inHours > 1 ? 's' : ''} ago';
    } else if (difference.inMinutes > 0) {
      return '${difference.inMinutes} minute${difference.inMinutes > 1 ? 's' : ''} ago';
    } else {
      return 'Just now';
    }
  }

  /// Format day of week (e.g., "Monday")
  static String formatDayOfWeek(DateTime date) {
    return DateFormat('EEEE').format(date);
  }

  // ============================================================================
  // DISTANCE FORMATTING
  // ============================================================================

  /// Format distance in kilometers
  static String formatDistance(double kilometers) {
    if (kilometers < 1) {
      final meters = (kilometers * 1000).round();
      return '$meters m';
    } else if (kilometers < 10) {
      return '${kilometers.toStringAsFixed(1)} km';
    } else {
      return '${kilometers.round()} km';
    }
  }

  /// Format distance in miles
  static String formatDistanceMiles(double miles) {
    if (miles < 0.1) {
      final feet = (miles * 5280).round();
      return '$feet ft';
    } else if (miles < 10) {
      return '${miles.toStringAsFixed(1)} mi';
    } else {
      return '${miles.round()} mi';
    }
  }

  // ============================================================================
  // DURATION FORMATTING
  // ============================================================================

  /// Format duration in minutes (e.g., "18 min")
  static String formatDuration(int minutes) {
    if (minutes < 60) {
      return '$minutes min';
    } else {
      final hours = minutes ~/ 60;
      final remainingMinutes = minutes % 60;
      if (remainingMinutes == 0) {
        return '$hours hr';
      } else {
        return '$hours hr $remainingMinutes min';
      }
    }
  }

  /// Format duration in seconds (e.g., "2:30")
  static String formatDurationSeconds(int seconds) {
    final minutes = seconds ~/ 60;
    final remainingSeconds = seconds % 60;
    return '$minutes:${remainingSeconds.toString().padLeft(2, '0')}';
  }

  /// Format hours with decimal (e.g., "2.5 hrs")
  static String formatHours(double hours) {
    return '${hours.toStringAsFixed(1)} hrs';
  }

  // ============================================================================
  // NUMBER FORMATTING
  // ============================================================================

  /// Format number with commas (e.g., "1,234")
  static String formatNumber(num number) {
    return NumberFormat('#,###').format(number);
  }

  /// Format number with decimal places
  static String formatDecimal(num number, {int decimalPlaces = 2}) {
    return number.toStringAsFixed(decimalPlaces);
  }

  /// Format as percentage (e.g., "95%")
  static String formatPercentage(double value, {int decimalPlaces = 0}) {
    return '${value.toStringAsFixed(decimalPlaces)}%';
  }

  /// Format rating (e.g., "4.8")
  static String formatRating(double rating) {
    return rating.toStringAsFixed(1);
  }

  // ============================================================================
  // PHONE FORMATTING
  // ============================================================================

  /// Format phone number (e.g., "+1 (555) 123-4567")
  static String formatPhone(String phone) {
    // Remove all non-digit characters
    final digits = phone.replaceAll(RegExp(r'\D'), '');

    if (digits.length == 10) {
      return '(${digits.substring(0, 3)}) ${digits.substring(3, 6)}-${digits.substring(6)}';
    } else if (digits.length == 11 && digits.startsWith('1')) {
      return '+1 (${digits.substring(1, 4)}) ${digits.substring(4, 7)}-${digits.substring(7)}';
    } else {
      return phone; // Return as-is if format doesn't match
    }
  }

  // ============================================================================
  // ADDRESS FORMATTING
  // ============================================================================

  /// Format address (truncate if too long)
  static String formatAddress(String address, {int maxLength = 50}) {
    if (address.length <= maxLength) {
      return address;
    }
    return '${address.substring(0, maxLength)}...';
  }

  // ============================================================================
  // VEHICLE FORMATTING
  // ============================================================================

  /// Format vehicle info (e.g., "2022 Toyota Camry - Black")
  static String formatVehicle({
    required int year,
    required String make,
    required String model,
    String? color,
  }) {
    final base = '$year $make $model';
    return color != null ? '$base - $color' : base;
  }

  /// Format license plate (uppercase)
  static String formatLicensePlate(String plate) {
    return plate.toUpperCase().replaceAll(RegExp(r'\s+'), ' ');
  }

  // ============================================================================
  // RIDE FORMATTING
  // ============================================================================

  /// Format fare amount
  static String formatFare(double amount, {String currency = 'USD'}) {
    return formatCurrency(amount, currency: currency);
  }

  /// Format ride type (capitalize)
  static String formatRideType(String type) {
    return type.split('_').map((word) {
      return word[0].toUpperCase() + word.substring(1);
    }).join(' ');
  }

  /// Format ride status
  static String formatRideStatus(String status) {
    return status.replaceAll('_', ' ').split(' ').map((word) {
      if (word.isEmpty) return word;
      return word[0].toUpperCase() + word.substring(1);
    }).join(' ');
  }

  // ============================================================================
  // FILE SIZE FORMATTING
  // ============================================================================

  /// Format file size (e.g., "2.5 MB")
  static String formatFileSize(int bytes) {
    if (bytes < 1024) {
      return '$bytes B';
    } else if (bytes < 1024 * 1024) {
      return '${(bytes / 1024).toStringAsFixed(1)} KB';
    } else if (bytes < 1024 * 1024 * 1024) {
      return '${(bytes / (1024 * 1024)).toStringAsFixed(1)} MB';
    } else {
      return '${(bytes / (1024 * 1024 * 1024)).toStringAsFixed(1)} GB';
    }
  }

  // ============================================================================
  // TRUNCATION
  // ============================================================================

  /// Truncate text with ellipsis
  static String truncate(String text, int maxLength) {
    if (text.length <= maxLength) {
      return text;
    }
    return '${text.substring(0, maxLength)}...';
  }

  /// Truncate middle of text (e.g., "John...Doe")
  static String truncateMiddle(String text, int maxLength) {
    if (text.length <= maxLength) {
      return text;
    }
    final half = (maxLength - 3) ~/ 2;
    return '${text.substring(0, half)}...${text.substring(text.length - half)}';
  }
}