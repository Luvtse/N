# Corrections — v1.0.0 Changelog

This document records retractions and corrections to `releases/v1.0.0/CHANGELOG.md`.
Per the ride-parity global rules (doc/CONTRIBUTING-RIDE-PARITY.md, Rule 7), no
changelog entry may describe a feature that was not actually on main at release time.

## 2026-10-12 — In-app chat claim retracted

**Original claim (v1.0.0 section, Nidus/Rides list):**
> In-app chat between rider and driver

**Correction:** In-app rider↔driver chat was **not shipped in v1.0.0**. At release
time there was no chat module in `backend/internal/modules/`, no chat endpoints,
and no chat surface in the Flutter client. The claim was inaccurate.

**Status:** Chat is scheduled for **v1.1.0** under ride-parity plan step §2.1
(backend `chat` module with Message entity/repository/service, REST endpoints
`POST /chat/rides/:rideId/messages` and `GET /chat/rides/:rideId/messages?cursor=`,
WS topic `chat.ride.{rideId}`, server-side canned messages per locale, and the
Flutter `chat_page.dart` reachable from the Tracking page driver card).

The v1.0.0 changelog line has been annotated to point here, and a corresponding
"Planned for v1.1.0" entry has been added so the roadmap reflects the true state.

## Verification trail

- `grep -ri "chat" backend/internal/modules/` returned no chat module at the time
  of this correction (Foundation phase, pre-§2.1).
- No `/chat` route is registered in the backend HTTP router.
- No chat page exists in `frontend/lib/features/nidus/presentation/pages/`.
