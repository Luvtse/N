# NIDAW Admin Console (Phase H Step 3)

React + TypeScript + Vite console for ledger operations staff.

## Pages
- **Dispute Queue** (`/disputes`) — open/admin_review disputes from the escrow engine; resolve with refund_rider / release_driver / split (notes mandatory, audited).
- **Withdrawal Approvals** (`/withdrawals`) — manual sign-off queue for fraud_hold + pending payouts; approve/reject with reason; live open-fraud-flag table.
- **Adjustments** (`/adjustments`) — force credit/debit with mandatory reason code; recent admin activity feed (`audit_log`).
- **Reconciliation** (`/reconciliation`) — daily provider mismatch dashboard (Telebirr / Chapa / M-Pesa); cached last tick by default, `?live=true` forces a fresh pass.

## API surface
Consumes `/api/v1/ledger/admin/*` endpoints (all RequireRole("admin")) implemented in
`backend/internal/modules/ledger/interfaces/http/handler.go`. Auth token is read from
`localStorage.nidaw_admin_token` and sent as `Authorization: Bearer …`.

## Run
```bash
npm install
npm run dev    # http://localhost:5174, proxies /api -> :8080 (override via VITE_API_PROXY_TARGET)
npm run build  # tsc -b && vite build
```
