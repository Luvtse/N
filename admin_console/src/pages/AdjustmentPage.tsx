import { useState } from 'react';
import { api, AuditEntry } from '../api/client';
import { ErrorNote, Panel, useAsync } from '../components/ui';

const REASON_CODES = [
  'GOODWILL_CREDIT',
  'FRAUD_REVERSAL',
  'DUPLICATE_CHARGE',
  'SUPPORT_COMPENSATION',
  'PROVIDER_FAILURE_ADJUSTMENT',
  'MANUAL_CORRECTION',
];

export default function AdjustmentPage() {
  const [tick, setTick] = useState(0);
  const audit = useAsync(() => api.auditLog(undefined, 50), [tick]);
  const [userId, setUserId] = useState('');
  const [amount, setAmount] = useState('');
  const [code, setCode] = useState(REASON_CODES[0]);
  const [text, setText] = useState('');
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  const canSubmit =
    /^[0-9a-fA-F-]{32,36}$/.test(userId.trim()) &&
    /^-?\d+(\.\d{1,2})?$/.test(amount.trim()) &&
    Number(amount) !== 0 &&
    text.trim().length > 0;

  const submit = async () => {
    if (!canSubmit) return;
    setBusy(true);
    setActionError(null);
    setResult(null);
    try {
      const res = await api.adjustment(userId.trim(), amount.trim(), code, text.trim());
      setResult(`Adjustment recorded. tx_id=${res.tx_id}`);
      setAmount('');
      setText('');
      setTick((t) => t + 1);
    } catch (e) {
      setActionError(String((e as Error).message));
    } finally {
      setBusy(false);
    }
  };

  const entries = audit.data?.entries ?? [];

  return (
    <>
      <Panel title="Force Credit / Debit (audited)">
        <p className="muted">
          Positive amounts credit the user, negative amounts debit. A reason code and explanatory
          text are mandatory — every adjustment is written to the immutable audit log.
        </p>
        <div className="form-grid">
          <label>
            User ID (UUID)
            <input value={userId} onChange={(e) => setUserId(e.target.value)} placeholder="…" />
          </label>
          <label>
            Amount (ETB)
            <input value={amount} onChange={(e) => setAmount(e.target.value)} placeholder="-25.00" />
          </label>
          <label>
            Reason code
            <select value={code} onChange={(e) => setCode(e.target.value)}>
              {REASON_CODES.map((c) => (
                <option key={c} value={c}>
                  {c}
                </option>
              ))}
            </select>
          </label>
          <label className="span2">
            Reason text (visible in audit trail)
            <textarea value={text} onChange={(e) => setText(e.target.value)} rows={2} />
          </label>
        </div>
        <div className="row">
          <button className="btn-ok" disabled={busy || !canSubmit} onClick={submit}>
            Submit adjustment
          </button>
        </div>
        {result && <p className="ok">{result}</p>}
        <ErrorNote message={actionError} />
      </Panel>

      <Panel
        title="Recent Admin Activity"
        actions={<button onClick={() => setTick((t) => t + 1)}>Refresh</button>}
      >
        <ErrorNote message={audit.error} />
        {audit.loading && <p>Loading…</p>}
        {!audit.loading && entries.length === 0 && <p className="muted">No admin actions yet.</p>}
        {entries.length > 0 && (
          <table>
            <thead>
              <tr>
                <th>When</th>
                <th>Actor</th>
                <th>Action</th>
                <th>Target</th>
                <th>Reason</th>
              </tr>
            </thead>
            <tbody>
              {entries.map((a: AuditEntry) => (
                <tr key={a.id}>
                  <td>{new Date(a.occurred_at).toLocaleString()}</td>
                  <td className="mono">{a.actor_user_id.slice(0, 8)}</td>
                  <td>
                    {a.action} <span className="muted">({a.actor_role})</span>
                  </td>
                  <td className="mono">
                    {a.target_type}
                    {a.target_id ? `:${a.target_id.slice(0, 8)}` : ''}
                  </td>
                  <td>
                    {a.reason_code ? <span className="badge">{a.reason_code}</span> : '—'}{' '}
                    <span className="muted">{a.reason_text}</span>
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
