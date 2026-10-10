# Contributing — Ride Parity (Nidus → Uber-Competitive)

These are the **global rules** every implementer must follow on this branch. They apply
to every step in every phase of the ride-parity build plan. A PR that violates any rule
below does not merge.

## Global Rules

1. **Full-file rewrites only.** When a step touches a file, the file is delivered in its
   entirety — imports, class bodies, closing brackets, trailing commas. No
   `// ... existing code ...`, no `/* TODO */`, no `# rest unchanged`.

2. **No placeholders in shipped surfaces.** If a value must be dynamic and the source is
   not ready, the UI shows a real "unavailable" state with retry — never a hardcoded
   number, never a fake string like `$12–15`, never NYC coordinates.

3. **Every new dependency is verified installed** in `pubspec.yaml` (Flutter) or
   `go.mod` (Go) before the file that imports it is committed. No orphan imports.

4. **Every new route is registered in the single router** (`app_router.dart` or
   equivalent). A file that exists but is unrouted is treated as a build failure, not a
   nice-to-have.

5. **Every new backend endpoint** is wired into the module's HTTP/WS handler, covered by
   at least one integration test, and referenced by the Flutter client in the same PR —
   or it does not merge.

6. **Every new feature flag** lives in `config/feature-flags/`, defaults to `false` in
   production, and has a paired kill-switch documented in the flag file.

7. **Every commit that changes a user-visible surface updates
   `releases/vX.Y.Z/CHANGELOG.md` in the same commit.** No changelog entries may describe
   features that are not on main.

## Phase Ordering (single sequence, no timelapse)

- **Foundation (blocks everything):** 1.1 Map → 1.2 Location → 1.3 Geocoding →
  1.4 Notifications → 1.5 Router cleanup.
- **Phase 1 (trust & comms):** 2.1 Chat → 2.2 Masked calling → 2.3 Pre-selfie →
  2.4 Driver card → 2.5 Cancellation quote → 2.6 Ride-option carousel.
- **Phase 2 (money & market fit):** 3.1 Cash + payment CRUD → 3.2 Promos → 3.3 Split fare
  → 3.4 Itemized receipt → 3.5 Stopovers + destination change → 3.6 Scheduled rides UI.
- **Phase 3 (differentiation):** 4.1 AR entry → 4.2 Voice GA → 4.3 Localization →
  4.4 Accessibility → 4.5 RideCheck → 4.6 Price extension.

Each numbered step is independently verifiable and independently revertible via its
feature flag. No step depends on a later step; every step depends only on earlier ones.

## Preserved Features (regression guard)

Wallet, SOS, PIN, dispute flow, and tips are peripheral features that must be preserved.
No step in this plan removes or regresses them.
