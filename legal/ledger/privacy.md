# NIDAW Ledger Privacy Notice

**Last Updated:** October 6, 2026

## Scope
This notice covers personal data processed by the internal ledger system: balance records, top-up/withdrawal requests, disputes, payout account details, and fraud-risk signals.

## Data Minimization
- The Ledger stores only: user ID, transaction type, amount, currency, timestamps, hash-chain links, and minimal metadata needed for reconciliation.
- No behavioral profiling data is written into immutable ledger entries.

## Encryption
- Bank/payout account details are encrypted at rest with AES-256-GCM; master keys are held in a secrets manager and rotated every 90 days.
- Payment-provider identifiers (Telebirr/Chapa/M-Pesa references) are stored pseudonymously where feasible.

## Immutability vs. Deletion Rights (GDPR Art. 17)
Ledger transactions are append-only for integrity. To honor deletion requests, NIDAW uses cryptographic erasure: the personal-data fields attached to an entry are destroyed and replaced with a salted hash placeholder, while the numeric transaction record (required for financial-law retention) remains unlinkable to you. See docs/security/ledger-security.md (Phase D).

## Retention
- Financial transaction records: retained per National Bank of Ethiopia reporting requirements (minimum statutory period).
- Dispute evidence files: retained 2 years post-resolution, then purged.
- Fraud risk scores: recalculated continuously; stale device/IP signals expire after 90 days.

## Sharing
Data is shared with payment providers only as required to complete a specific top-up or payout, and with regulators upon lawful request. We do not sell ledger data.

## Access & Portability
Users can export their full transaction history (with hash chain) from the app at any time.
