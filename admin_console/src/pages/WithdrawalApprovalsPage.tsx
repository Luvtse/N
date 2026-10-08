import { useState } from 'react';
import { api, FraudFlag, PendingWithdrawal } from '../api/client';
import { ErrorNote, Panel, StatusBadge, useAsync } from '../components/ui';

export default function WithdrawalApprovalsPage() {
  const [tick, setTick] = useState(0);
  const withdrawals = useAsync(() => api.pendingWithdrawals(50), [tick]);
  const flags = useAsync(
    () => api.fraudFlags(50).catch(() => ({ count: 0, fraud_flags: [] as FraudFlag[] })),
    [tick],
  );
  const [rejectId, setRejectId] = useState<string | null>(null);
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);

  const act = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    setActionError(null);
    try {
      await fn();
      setRejectId(null);
      setReason('');
      setTick((t) => t + 1);
    } catch (e) {
      setActionError(String((e as Error).message));
    } finally {
      setBusy(false);
    }
  };

  const rows = withdrawals.data?.withdrawals ?? [];

  return (
    <>
      <Panel
        title="Withdrawal Approvals"
        actions={
          <div className="row">
            {withdrawals.data ? <span className="badge badge-warn">{withdrawals.data.requires_action} on fraud hold</span> : null}
            <button onClick={() => setTick((t) => t + 1)}>Refresh</button>
          </div>
        }
      >
        <ErrorNote message={withdrawals.error ?? actionError} />
        {withdrawals.loading && <p>Loading…</p>}
        {!withdrawals.loading && rows.length === 0 && <p className="muted">Nothing waiting for sign-off.</p>}
        {rows.length > 0 && (
          <table>
            <thead>
              <tr>
                <th>User</th>
                <th>Amount (ETB)</th>
                <th>Fee</th>
                <th>Destination</th>
                <th>Risk</th>
                <th>Hold reason</th>
                <th>Status</th>
                <th>Requested</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {rows.map((w: PendingWithdrawal) => (
                <tr key={w.withdrawal_id}>
                  <td className="mono">{w.user_id.slice(0, 8)}</td>
                  <td>{w.amount_etb}</td>
                  <td>{w.fee_etb}</td>
                  <td className="mono">{w.destination_type}</td>
                  <td>{w.risk_score?.toFixed(2) ?? '—'}</td>
                  <td>{w.fraud_hold_reason ?? '—'}</td>
                  <td><StatusBadge status={w.status} /></td>
                  <td>{new Date(w.requested_at).toLocaleString()}</td>
                  <td className="row">
                    <button
                      disabled={busy}
                      className="btn-ok"
                      onClick={() => act(() => api.approveWithdrawal(w.withdrawal_id))}
                    >
                      Approve
                    </button>
                    <button disabled={busy} onClick={() => setRejectId(rejectId === w.withdrawal_id ? null : w.withdrawal_id)}>
                      Reject
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        {rejectId && (
          <div className="resolve-form">
            <label>
              Rejection reason (audited)
              <textarea value={reason} onChange={(e) => setReason(e.target.value)} rows={2} />
            </label>
            <button
              disabled={busy || !reason.trim()}
              onClick={() => act(() => api.rejectWithdrawal(rejectId, reason))}
            >
              Confirm rejection
            </button>
          </div>
        )}
      </Panel>

      <Panel title="Open Fraud Flags">
        {flags.error && <ErrorNote message={flags.error} />}
        {!flags.loading && (flags.data?.fraud_flags.length ?? 0) === 0 && (
          <p className="muted">No open fraud flags (or Phase G not enabled).</p>
        )}
        {(flags.data?.fraud_flags.length ?? 0) > 0 && (
          <table>
            <thead>
              <tr>
                <th>User</th>
                <th>Check</th>
                <th>Severity</th>
                <th>Risk score</th>
                <th>Entity</th>
              </tr>
            </thead>
            <tbody>
              {flags.data!.fraud_flags.map((f: FraudFlag) => (
                <tr key={f.flag_id}>
                  <td className="mono">{f.user_id.slice(0, 8)}</td>
                  <td>{f.check_type}</td>
                  <td><StatusBadge status={f.severity} /></td>
                  <td>{f.risk_score.toFixed(2)}</td>
                  <td className="mono">
                    {f.entity_type}
                    {f.entity_id ? `:${f.entity_id.slice(0, 8)}` : ''}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>
    </>
  );
}
