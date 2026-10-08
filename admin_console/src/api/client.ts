// NIDAW Admin Console — typed API client for the ledger admin endpoints.
// All paths are relative to the gateway (/api/v1/ledger/...). Identity comes
// from the JWT bearer token; server rejects body-supplied user ids (Phase B1).

const BASE = import.meta.env.VITE_API_BASE ?? '/api/v1/ledger';

export class ApiError extends Error {
  constructor(public status: number, public code: string, message: string) {
    super(message);
  }
}

function getToken(): string | null {
  // Admin console uses a session-scoped token stored by the login flow.
  return window.localStorage.getItem('nidaw_admin_token');
}

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const token = getToken();
  const res = await fetch(`${BASE}${path}`, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...(init?.headers ?? {}),
    },
  });
  if (!res.ok) {
    let code = 'UNKNOWN';
    let message = res.statusText;
    try {
      const body = await res.json();
      code = body.code ?? code;
      message = body.message ?? message;
    } catch {
      /* non-JSON error body */
    }
    throw new ApiError(res.status, code, message);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

// ---------------------------------------------------------------- types ----

/** Mirrors handler.go withdrawalView (handlePendingWithdrawals). */
export interface PendingWithdrawal {
  withdrawal_id: string;
  user_id: string;
  amount_etb: string;
  fee_etb: string;
  destination_type: string;
  status: string;
  risk_score?: number;
  fraud_hold_reason?: string;
  requested_at: string;
  metadata?: Record<string, unknown>;
}

/** Mirrors handler.go flagView (handleFraudFlags). */
export interface FraudFlag {
  flag_id: string;
  user_id: string;
  check_type: string;
  severity: string;
  risk_score: number;
  entity_type: string;
  entity_id?: string;
  details?: Record<string, unknown>;
}

/** Mirrors handler.go entryView (handleAuditLog). */
export interface AuditEntry {
  id: string;
  actor_user_id: string;
  actor_role: string;
  action: string;
  target_type: string;
  target_id?: string;
  reason_code?: string;
  reason_text?: string;
  ip_address?: string;
  occurred_at: string;
}

/** Mirrors handler.go discView (handleReconciliation). */
export interface ReconDiscrepancy {
  kind: string;
  provider: string;
  request_id: string;
  user_id: string;
  ours_etb: string;
  theirs_etb: string;
  our_status: string;
  their_status: string;
  reference?: string;
}

export interface ReconciliationReport {
  available: boolean;
  note?: string;
  ran_at?: string;
  window_start?: string;
  checked_topups?: number;
  checked_payouts?: number;
  errors?: number;
  discrepancies: ReconDiscrepancy[];
}

/** Mirrors handler.go disputeView (handleDisputeQueue). */
export interface Dispute {
  dispute_id: string;
  ride_id: string;
  hold_id: string;
  filed_by_user_id: string;
  against_user_id: string;
  reason_code: string;
  description: string;
  evidence_urls?: string[];
  status: string;
  created_at: string;
  resolved_at?: string;
}

// ------------------------------------------------------------- endpoints ----

export const api = {
  pendingWithdrawals: (limit = 50) =>
    req<{ count: number; requires_action: number; withdrawals: PendingWithdrawal[] }>(
      `/admin/withdrawals/pending?limit=${limit}`,
    ),

  approveWithdrawal: (id: string) =>
    req<{ ok: boolean }>(`/withdrawals/${id}/approve`, { method: 'POST' }),

  rejectWithdrawal: (id: string, reason: string) =>
    req<{ ok: boolean }>(`/withdrawals/${id}/reject`, {
      method: 'POST',
      body: JSON.stringify({ reason }),
    }),

  fraudFlags: (limit = 50) =>
    req<{ count: number; fraud_flags: FraudFlag[] }>(`/admin/fraud-flags?limit=${limit}`),

  auditLog: (action?: string, limit = 100) =>
    req<{ count: number; action_filter: string; entries: AuditEntry[] }>(
      `/admin/audit-log?limit=${limit}${action ? `&action=${encodeURIComponent(action)}` : ''}`,
    ),

  reconciliation: (live = false) =>
    req<ReconciliationReport>(`/admin/reconciliation${live ? '?live=true' : ''}`),

  disputeQueue: (all = false, limit = 50) =>
    req<{ pending_only: boolean; count: number; disputes: Dispute[] }>(
      `/disputes?limit=${limit}${all ? '&all=true' : ''}`,
    ),

  resolveDispute: (
    id: string,
    outcome: 'refund_rider' | 'release_driver' | 'split',
    notes: string,
    refundEtb?: string,
  ) =>
    req<{ ok: boolean }>(`/disputes/${id}/resolve`, {
      method: 'POST',
      body: JSON.stringify({ outcome, notes, refund_etb: refundEtb }),
    }),

  adjustment: (userId: string, amountEtb: string, reasonCode: string, reasonText: string) =>
    req<{ tx_id: string }>('/adjustments', {
      method: 'POST',
      body: JSON.stringify({
        user_id: userId,
        amount_etb: amountEtb,
        reason_code: reasonCode,
        reason_text: reasonText,
      }),
    }),
};
