import { useState } from 'react';
import { api, ReconDiscrepancy } from '../api/client';
import { ErrorNote, Panel, useAsync } from '../components/ui';

export default function ReconciliationPage() {
  const [live, setLive] = useState(false);
  const [tick, setTick] = useState(0);
  const { data, error, loading } = useAsync(() => api.reconciliation(live), [live, tick]);

  const discs = data?.discrepancies ?? [];
  const hasMismatch = discs.length > 0;

  return (
    <Panel
      title="Provider Reconciliation"
      actions={
        <div className="row">
          <label className="inline">
            <input type="checkbox" checked={live} onChange={(e) => setLive(e.target.checked)} />
            Run live check
          </label>
          <button onClick={() => setTick((t) => t + 1)}>Refresh</button>
        </div>
      }
    >
      <ErrorNote message={error} />
      {loading && <p>Loading… (live checks query Telebirr/Chapa/M-Pesa and may take a moment)</p>}
      {data && !data.available && <p className="muted">{data.note ?? 'No reconciliation pass has run yet on this instance.'}</p>}
      {data && data.available && (
        <>
          <p className="muted">
            Last run {data.ran_at ? new Date(data.ran_at).toLocaleString() : '—'} · window since{' '}
            {data.window_start ? new Date(data.window_start).toLocaleString() : '—'} ·{' '}
            {data.checked_topups ?? 0} top-ups + {data.checked_payouts ?? 0} payouts checked ·{' '}
            {data.errors ?? 0} provider errors
          </p>
          {!hasMismatch ? (
            <p className="ok">✅ No discrepancies above the 0.01 ETB tolerance.</p>
          ) : (
            <table>
              <thead>
                <tr>
                  <th>Kind</th>
                  <th>Provider</th>
                  <th>Request</th>
                  <th>User</th>
                  <th>Ours (ETB)</th>
                  <th>Theirs (ETB)</th>
                  <th>Our status</th>
                  <th>Their status</th>
                  <th>Reference</th>
                </tr>
              </thead>
              <tbody>
                {discs.map((d: ReconDiscrepancy, i: number) => (
                  <tr key={`${d.request_id}-${i}`} className="row-warn">
                    <td>{d.kind}</td>
                    <td>{d.provider}</td>
                    <td className="mono">{d.request_id.slice(0, 8)}</td>
                    <td className="mono">{d.user_id.slice(0, 8)}</td>
                    <td>{d.ours_etb}</td>
                    <td>{d.theirs_etb}</td>
                    <td>{d.our_status}</td>
                    <td>{d.their_status}</td>
                    <td className="mono">{d.reference ?? '—'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </>
      )}
    </Panel>
  );
}
