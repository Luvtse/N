import { ReactNode } from 'react';
import { NavLink } from 'react-router-dom';

const NAV = [
  { to: '/disputes', label: 'Dispute Queue' },
  { to: '/withdrawals', label: 'Withdrawal Approvals' },
  { to: '/adjustments', label: 'Adjustments' },
  { to: '/reconciliation', label: 'Reconciliation' },
];

export default function Layout({ children }: { children: ReactNode }) {
  return (
    <div className="shell">
      <header className="topbar">
        <span className="brand">NIDAW · Admin Console</span>
        <nav>
          {NAV.map((n) => (
            <NavLink key={n.to} to={n.to} className={({ isActive }) => (isActive ? 'active' : '')}>
              {n.label}
            </NavLink>
          ))}
        </nav>
      </header>
      <main className="content">{children}</main>
    </div>
  );
}
