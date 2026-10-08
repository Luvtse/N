import { useState } from 'react';
import { api, Dispute } from '../api/client';
import { ErrorNote, Panel, StatusBadge, useAsync } from '../components/ui';

type Outcome = 'refund_rider' | 'release_driver' | 'split';

export default function DisputeQueuePage() {
  const [tick, setTick] = useState(0);
  const [showAll, setShowAll] = useState(false);
  const { data, error, loading } = useAsync(() => api.disputeQueue(showAll, 50), [showAll, tick]);
  const [selected, setSelected] = useState<string | null>(null);
  const [notes, setNotes] = useState('');
  const [busy, setBusy] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);

  const resolve = async (outcome: Outcome) => {
    if (!selected) return;
    setBusy(true);
    setActionError(null);
    try {
      await api.resolveDispute(selected, outcome, notes.trim());
      setSelected(null);
      setNotes('');
      setTick((t) => t + 1);
    } catch (e) {
      setActionError(String((e as Error).message));
    } finally {
      setBusy(false);
    }
  };

  const items = data?.disputes ?? [];

  return (
    <Panel
      title="Dispute Queue"
      actions={
        <div className="row">
          <label className="inline">
            <input type="checkbox" checked={showAll} onChange={(e) => setShowAll(e.target.checked)} />
            Include resolved
          </label>
          <button onClick={() => setTick((t) => t + 1)}>Refresh</button>
        </div>
      }
    >
      <ErrorNote message={error ?? actionError} />
      {loading && <p>Loading…</p>}
      {!loading && items.length === 0 && <p className="muted">No disputes needing review. 🎉</p>}
      {items.length > 0 && (
        <table>
          <thead>
            <tr>
              <th>Ride</th>
              <th>Filed by</th>
              <th>Against</th>
              <th>Reason</th>
              <th>Description</th>
              <th>Status</th>
              <th>Filed</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {items.map((d: Dispute) => (
              <tr key={d.dispute_id} className={selected === d.dispute_id ? 'selected' : ''}>
                <td className="mono">{d.ride_id.slice(0, 8)}</td>
                <td className="mono">{d.filed_by_user_id.slice(0, 8)}</td>
                <td className="mono">{d.against_user_id.slice(0, 8)}</td>
                <td>{d.reason_code}</td>
                <td>{d.description}</td>
                <td><StatusBadge status={d.status} /></td>
                <td>{new Date(d.created_at).toLocaleString()}</td>
                <td>
                  {d.status === 'open' || d.status === 'admin_review' ? (
                    <button onClick={() => setSelected(selected === d.dispute_id ? null : d.dispute_id)}>
                      {selected === d.dispute_id ? 'Close' : 'Review'}
                    </button>
                  ) : (
                    <span className="muted">{d.resolved_at ? new Date(d.resolved_at).toLocaleString() : '—'}</span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {selected && (
        <div className="resolve-form">
          <label>
            Resolution notes (required for audit trail)
            <textarea value={notes} onChange={(e) => setNotes(e.target.value)} rows={3} />
          </label>
          {(data?.disputes ?? []).find((d) => d.dispute_id === selected)?.evidence_urls?.length ? (
            <p className="muted">
              Evidence:{' '}
              {(data?.disputes ?? [])
                .find((d) => d.dispute_id === selected)!
                .evidence_urls!.map((u, i) => (
                  <a key={i} href={u} target="_blank" rel="noreferrer" style={{ marginRight: 8 }}>
                    [{i + 1}]
                  </a>
                ))}
            </p>
          ) : null}
          <div className="row">
            <button disabled={busy || !notes.trim()} onClick={() => resolve('refund_rider')}>
              Refund rider
            </button>
            <button disabled={busy || !notes.trim()} onClick={() => resolve('release_driver')}>
              Release to driver
            </button>
            <button disabled={busy || !notes.trim()} onClick={() => resolve('split')}>
              Split 50/50
            </button>
          </div>
        </div>
      )}
    </Panel>
  );
}
