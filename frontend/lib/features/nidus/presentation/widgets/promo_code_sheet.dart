import 'package:flutter/material.dart';

import '../../../../core/di/injection.dart';
import '../../../../core/theme/app_colors.dart';
import '../../data/repositories/phase2_money_repository.dart';

/// §3.2 Promo Codes & Coupons — entry sheet on the Request page.
///
/// Two ways to attach a code: type one in (validated against
/// POST /promotions/redeem with the current estimated fare) or pick from
/// GET /promotions/mine. The projected discount returned by the server is
/// what [onApplied] receives; nothing is ever computed client-side, and a
/// failed validation shows the server's reason instead of silently keeping
/// a stale code (Global Rule: no fabricated numbers).
class PromoCodeSheet extends StatefulWidget {
  /// Current estimated fare used for the redeem projection. When null the
  /// "Apply" button stays disabled with an explanatory hint — projecting a
  /// discount against an unknown fare would be fabrication.
  final double? estimatedFare;
  final String currency;
  final String rideType;

  /// Currently attached code (if any), shown pre-filled so the rider can
  /// clear it.
  final String? currentCode;

  /// Called with the normalized code + server-projected discount when a
  /// validation succeeds, or with (null, null) when the rider clears the
  /// promo.
  final void Function(String? code, double? projectedDiscount) onApplied;

  const PromoCodeSheet({
    super.key,
    required this.estimatedFare,
    required this.currency,
    required this.rideType,
    required this.onApplied,
    this.currentCode,
  });

  static Future<void> show(
    BuildContext context, {
    required double? estimatedFare,
    required String currency,
    required String rideType,
    String? currentCode,
    required void Function(String? code, double? projectedDiscount) onApplied,
  }) {
    return showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      backgroundColor: AppColors.surface,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
      ),
      builder: (_) => Padding(
        padding: EdgeInsets.only(
          bottom: MediaQuery.of(context).viewInsets.bottom,
        ),
        child: PromoCodeSheet(
          estimatedFare: estimatedFare,
          currency: currency,
          rideType: rideType,
          currentCode: currentCode,
          onApplied: onApplied,
        ),
      ),
    );
  }

  @override
  State<PromoCodeSheet> createState() => _PromoCodeSheetState();
}

class _PromoCodeSheetState extends State<PromoCodeSheet> {
  final TextEditingController _controller = TextEditingController();
  final Phase2MoneyRepository _repo = getIt<Phase2MoneyRepository>();

  List<PromoCodeInfo>? _myCodes;
  bool _loadingMine = false;
  bool _validating = false;
  String? _validationError;

  @override
  void initState() {
    super.initState();
    if (widget.currentCode != null) {
      _controller.text = widget.currentCode!;
    }
    _loadMine();
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  Future<void> _loadMine() async {
    setState(() => _loadingMine = true);
    final result = await _repo.listMyPromos();
    if (!mounted) return;
    setState(() {
      _loadingMine = false;
      // Honest empty/unavailable states are rendered below; we never show a
      // fake list when the endpoint failed.
      _myCodes = result.ok ? result.codes : null;
    });
  }

  Future<void> _validateAndApply(String code) async {
    final trimmed = code.trim();
    if (trimmed.isEmpty) {
      setState(() => _validationError = 'Enter a promo code first.');
      return;
    }
    final fare = widget.estimatedFare;
    if (fare == null) {
      setState(() => _validationError =
          'Set pickup and destination to see a fare before applying a code.');
      return;
    }
    setState(() {
      _validating = true;
      _validationError = null;
    });
    final result = await _repo.redeemPromo(
      code: trimmed,
      fare: fare,
      currency: widget.currency,
      rideType: widget.rideType,
    );
    if (!mounted) return;
    setState(() => _validating = false);
    if (result.ok && result.projection != null) {
      widget.onApplied(result.projection!.code,
          result.projection!.projectedDiscount);
      Navigator.of(context).pop();
    } else {
      setState(() => _validationError =
          result.message ?? 'This code could not be validated.');
    }
  }

  void _clear() {
    widget.onApplied(null, null);
    Navigator.of(context).pop();
  }

  @override
  Widget build(BuildContext context) {
    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              mainAxisAlignment: MainAxisAlignment.spaceBetween,
              children: [
                const Text(
                  'Promo code',
                  style: TextStyle(
                    fontSize: 18,
                    fontWeight: FontWeight.bold,
                    color: AppColors.textPrimary,
                  ),
                ),
                if ((widget.currentCode ?? '').isNotEmpty)
                  TextButton(
                    onPressed: _validating ? null : _clear,
                    child: const Text('Remove'),
                  ),
              ],
            ),
            const SizedBox(height: 12),
            TextField(
              controller: _controller,
              textCapitalization: TextCapitalization.characters,
              enabled: !_validating,
              decoration: InputDecoration(
                hintText: 'e.g. NIDUS20',
                border: const OutlineInputBorder(),
                prefixIcon: const Icon(Icons.local_offer_outlined),
                errorText: _validationError,
              ),
              onSubmitted: _validateAndApply,
            ),
            const SizedBox(height: 12),
            SizedBox(
              width: double.infinity,
              child: ElevatedButton.icon(
                onPressed: _validating
                    ? null
                    : () => _validateAndApply(_controller.text),
                icon: _validating
                    ? const SizedBox(
                        width: 16,
                        height: 16,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : const Icon(Icons.check),
                label: Text(_validating
                    ? 'Checking…'
                    : 'Validate & apply'),
              ),
            ),
            if (widget.estimatedFare == null) ...[
              const SizedBox(height: 8),
              const Text(
                'Needs an estimated fare first — choose pickup and dropoff.',
                style:
                    TextStyle(fontSize: 12, color: AppColors.textSecondary),
              ),
            ],
            const SizedBox(height: 16),
            const Text(
              'Your codes',
              style: TextStyle(
                fontSize: 14,
                fontWeight: FontWeight.w600,
                color: AppColors.textPrimary,
              ),
            ),
            const SizedBox(height: 8),
            if (_loadingMine)
              const Center(
                child: Padding(
                  padding: EdgeInsets.all(12),
                  child: CircularProgressIndicator(
                      color: AppColors.primary),
                ),
              )
            else if (_myCodes == null)
              Row(
                children: [
                  const Expanded(
                    child: Text(
                      'Could not load your saved codes.',
                      style: TextStyle(
                          fontSize: 13, color: AppColors.textSecondary),
                    ),
                  ),
                  TextButton(
                    onPressed: _loadMine,
                    child: const Text('Retry'),
                  ),
                ],
              )
            else if (_myCodes!.isEmpty)
              const Text(
                'No active codes yet — new ones arrive as notifications.',
                style: TextStyle(fontSize: 13, color: AppColors.textSecondary),
              )
            else
              Flexible(
                child: ListView.builder(
                  shrinkWrap: true,
                  itemCount: _myCodes!.length,
                  itemBuilder: (_, i) {
                    final c = _myCodes![i];
                    return ListTile(
                      dense: true,
                      contentPadding: EdgeInsets.zero,
                      leading: const Icon(Icons.confirmation_number_outlined),
                      title: Text(c.code),
                      subtitle: Text(_describe(c)),
                      trailing: _validating
                          ? null
                          : TextButton(
                              onPressed: () => _validateAndApply(c.code),
                              child: const Text('Use'),
                            ),
                    );
                  },
                ),
              ),
            const SizedBox(height: 8),
          ],
        ),
      ),
    );
  }

  String _describe(PromoCodeInfo c) {
    final parts = <String>[];
    if (c.type == 'percent') {
      parts.add('${c.value.toStringAsFixed(0)}% off');
      if (c.maxDiscount != null) {
        parts.add('max ${widget.currency} ${c.maxDiscount!.toStringAsFixed(2)}');
      }
    } else {
      parts.add('${widget.currency} ${c.value.toStringAsFixed(2)} off');
    }
    if (c.minFare != null && c.minFare! > 0) {
      parts.add('min fare ${widget.currency} ${c.minFare!.toStringAsFixed(2)}');
    }
    if (c.validTo != null) {
      parts.add('until ${c.validTo!.toLocal().toString().split(' ').first}');
    }
    return parts.join(' • ');
  }
}
