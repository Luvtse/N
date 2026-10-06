# NIDAW Internal Ledger Terms and Conditions

**Last Updated:** October 6, 2026
**Status:** Supersedes all prior blockchain/token terms (see archive note below).

## 1. Introduction
These Terms govern your use of the NIDAW internal balance system ("the Ledger"), a private, permissioned transaction ledger operated entirely by NIDAW within its platform. The Ledger is an **internal settlement layer** — it is NOT a cryptocurrency, public blockchain, or digital asset system.

## 2. What the Ledger Is
- A closed-loop, fiat-backed record of your account balances in Ethiopian Birr (ETB) and supported currencies.
- An immutable, cryptographically-linked transaction log (each entry is hash-chained to the previous entry for your account) that provides auditability and tamper evidence.
- A convenience mechanism enabling instant ride payment, escrow protection, and fast refunds/dispute resolution.

## 3. What the Ledger Is NOT
- It is NOT a crypto wallet. There are no seed phrases, private keys held by you, gas fees, or token transfers outside the platform.
- Balances are NOT tokens, securities, or investment instruments. They carry no speculative value and cannot be traded, staked, mined, or exchanged outside the platform.
- No decentralized governance, mining, validators, or on-chain settlement exists or is offered.

## 4. Balances and Fiat Backing
Your available balance represents prepaid funds (via Telebirr, Chapa, M-Pesa Ethiopia, or bank transfer) held by NIDAW for your future use of platform services. Pending balances reflect in-flight top-ups; held balances reflect escrow amounts during dispute windows.

## 5. Dispute Resolution
Ride payments enter a 72-hour escrow hold after completion. During this window you may file a dispute with evidence. Automated rules resolve straightforward cases; complex cases receive manual review. If no dispute is filed, funds release to the service provider automatically. Full mechanics: see docs/architecture/dispute-resolution.md (Phase D).

## 6. Withdrawals
Withdrawal of earned balances is available only to verified service providers (drivers, hotels, restaurants, freight carriers) subject to identity verification, configurable limits, and fraud screening. Riders may not withdraw; rider balances are spendable within the platform or refundable to the original funding method.

## 7. Auditability
You may request a signed audit report of your transaction history and current balance at any time. Reports are cryptographically verifiable against NIDAW's published verification key, allowing independent confirmation without trusting the platform UI.

## 8. Adjustments & Negative Locks
If a top-up fails after provisional credit, NIDAW will append a corrective adjustment entry and may temporarily restrict new bookings until the account is restored to a non-negative state. All such actions are recorded immutably in the Ledger.

## 9. Archive Note
Prior versions of these terms referenced ERC-20 tokens, staking, and Ethereum smart contracts. Those features were never launched and have been removed from the codebase. Any legacy documents under legal/ledger/ predating this revision are retained for historical reference only and carry no force.
