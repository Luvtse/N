import { Navigate, Route, Routes } from 'react-router-dom';
import Layout from './components/Layout';
import DisputeQueuePage from './pages/DisputeQueuePage';
import WithdrawalApprovalsPage from './pages/WithdrawalApprovalsPage';
import AdjustmentPage from './pages/AdjustmentPage';
import ReconciliationPage from './pages/ReconciliationPage';

export default function App() {
  return (
    <Layout>
      <Routes>
        <Route path="/" element={<Navigate to="/disputes" replace />} />
        <Route path="/disputes" element={<DisputeQueuePage />} />
        <Route path="/withdrawals" element={<WithdrawalApprovalsPage />} />
        <Route path="/adjustments" element={<AdjustmentPage />} />
        <Route path="/reconciliation" element={<ReconciliationPage />} />
        <Route path="*" element={<Navigate to="/disputes" replace />} />
      </Routes>
    </Layout>
  );
}
