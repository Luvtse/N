# Phase 3 — Tracked Follow-ups (per Global Rule: no silent drift)

Every item below is a tracked follow-up, not a placeholder in shipped code.
Shipped surfaces degrade honestly; these are completeness/consistency items.

1. **§4.4 residual `Colors.white`** — remaining sites are deliberate
   white-on-dark overlay foregrounds (`safety_widgets.dart`, star glyphs) or
   peripheral pre-Phase-3 widgets. Decision recorded per site when converted;
   sweep of safety_widgets.dart onto `AppColors.textOnDark` deferred to the
   accessibility QA ticket (TalkBack/VoiceOver journey run, device-side CI).
2. **§4.3 deferred literals** — `kRideCancellationReasons` list copy,
   "GPS unavailable — drag the pickup pin…" banner, and
   "No saved methods yet…" empty state remain English-only in code with ARB
   keys pending translation sign-off; extraction is mechanical once the
   Amharic/Oromo strings are approved.
3. **CI-only verification** — `flutter gen-l10n` + `dart analyze` over the
   touched files (no Flutter SDK in this build environment). ARB catalogs are
   JSON-validated, 135-key parity across en/am/om (updated by the follow-up
   polish pass: historyTitle/historyEmpty added), all call-site placeholder
   arity checked against @-metadata (named args match placeholder names).
4. **Price-extension rollout gate** — server route unmounts entirely when the
   `price_extension` flag is off (404, never half-working); client keeps the
   legacy pop-back. Both must be enabled together via the documented kill
   switch in config/feature-flags/flags.json (and the bundled
   frontend/assets/config mirror).
