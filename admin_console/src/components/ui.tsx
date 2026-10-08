import { ReactNode, useEffect, useState } from 'react';

/** Generic async-data hook with manual refetch via deps change. */
export function useAsync<T>(fn: () => Promise<T>, deps: unknown[] = []) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    fn()
      .then((d) => {
        if (!cancelled) {
          setData(d);
          setError(null);
        }
      })
      .catch((e) => {
        if (!cancelled) setError(String(e?.message ?? e));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);

  return { data, error, loading };
}

export function StatusBadge({ status }: { status: string }) {
  const cls =
    status === 'open' || status === 'fraud_hold'
      ? 'badge badge-warn'
      : status === 'pending'
        ? 'badge badge-info'
        : status === 'completed' || status === 'approved'
          ? 'badge badge-ok'
          : 'badge';
  return <span className={cls}>{status}</span>;
}

export function Panel({
  title,
  actions,
  children,
}: {
  title: string;
  actions?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="panel">
      <div className="panel-head">
        <h2>{title}</h2>
        <div className="panel-actions">{actions}</div>
      </div>
      {children}
    </section>
  );
}

export function ErrorNote({ message }: { message: string | null }) {
  if (!message) return null;
  return <p className="error">{message}</p>;
}
