import os
import json
import pandas as pd
import numpy as np
from evidently.report import Report
from evidently.metric_preset import DataDriftPreset, TargetDriftPreset
from evidently.metrics import DatasetDriftMetric, ColumnDriftMetric
import mlflow
from datetime import datetime, timedelta
import psycopg2
import requests

class ModelDriftDetector:
    def __init__(self, model_name: str, reference_window_days=30, current_window_days=7):
        self.model_name = model_name
        self.reference_window_days = reference_window_days
        self.current_window_days = current_window_days
        self.db_url = os.getenv("DATABASE_URL")
        self.alert_webhook = os.getenv("ALERT_WEBHOOK_URL")
    
    def fetch_data(self, query: str, start_date: datetime, end_date: datetime) -> pd.DataFrame:
        """Fetch data from PostgreSQL"""
        conn = psycopg2.connect(self.db_url)
        query_with_dates = query + f" WHERE created_at BETWEEN '{start_date}' AND '{end_date}'"
        df = pd.read_sql(query_with_dates, conn)
        conn.close()
        return df
    
    def detect_drift(self):
        """Run drift detection on model features and predictions"""
        now = datetime.now()
        reference_start = now - timedelta(days=self.reference_window_days + self.current_window_days)
        reference_end = now - timedelta(days=self.current_window_days)
        current_start = now - timedelta(days=self.current_window_days)
        current_end = now
        
        # Fetch reference and current data
        reference_data = self.fetch_data(
            "SELECT * FROM ride_predictions", reference_start, reference_end
        )
        current_data = self.fetch_data(
            "SELECT * FROM ride_predictions", current_start, current_end
        )
        
        if len(current_data) < 100:
            print("⚠️ Not enough current data for drift detection")
            return
        
        # Create Evidently report
        report = Report(metrics=[
            DatasetDriftMetric(),
            ColumnDriftMetric(column_name="predicted_eta"),
            ColumnDriftMetric(column_name="traffic_encoded"),
            ColumnDriftMetric(column_name="demand_supply_ratio"),
        ])
        
        report.run(reference_data=reference_data, current_data=current_data)
        
        # Get drift score
        drift_score = report.as_dict()['metrics'][0]['result']['dataset_drift']
        
        # Log to MLflow
        with mlflow.start_run(run_name=f"drift_detection_{self.model_name}"):
            mlflow.log_metric("drift_score", drift_score)
            mlflow.log_metric("current_sample_size", len(current_data))
            
            # Save report
            report.save_html("/tmp/drift_report.html")
            mlflow.log_artifact("/tmp/drift_report.html")
        
        # Alert if drift detected
        if drift_score > 0.5:  # 50% of features drifted
            self._send_alert(drift_score, current_data)
            return True, drift_score
        
        return False, drift_score
    
    def _send_alert(self, drift_score: float, data: pd.DataFrame):
        """Send alert to monitoring system"""
        alert = {
            "model": self.model_name,
            "severity": "HIGH" if drift_score > 0.7 else "MEDIUM",
            "drift_score": drift_score,
            "timestamp": datetime.now().isoformat(),
            "sample_size": len(data),
            "message": f"Model {self.model_name} drift detected: {drift_score:.2%}",
        }
        
        # Send to webhook (PagerDuty, Slack, etc.)
        if self.alert_webhook:
            requests.post(self.alert_webhook, json=alert)
        
        print(f"🚨 DRIFT ALERT: {alert['message']}")
    
    def check_retraining_trigger(self) -> bool:
        """Check if model needs retraining based on performance degradation"""
        # Fetch recent predictions with actual outcomes
        query = """
            SELECT predicted_eta, actual_eta, created_at
            FROM ride_predictions
            WHERE actual_eta IS NOT NULL
            AND created_at > NOW() - INTERVAL '7 days'
        """
        df = self.fetch_data(query, datetime.now() - timedelta(days=7), datetime.now())
        
        if len(df) < 100:
            return False
        
        # Calculate MAE
        mae = np.mean(np.abs(df['predicted_eta'] - df['actual_eta']))
        
        # Compare to baseline (stored in MLflow)
        baseline_mae = self._get_baseline_mae()
        
        degradation = (mae - baseline_mae) / baseline_mae
        
        if degradation > 0.15:  # 15% degradation
            print(f"⚠️ Model performance degraded by {degradation:.2%}. Triggering retraining.")
            self._trigger_retraining()
            return True
        
        return False
    
    def _get_baseline_mae(self) -> float:
        """Get baseline MAE from MLflow model registry"""
        client = mlflow.MlflowClient()
        latest_model = client.get_latest_versions("nidaw-eta-predictor", stages=["Production"])[0]
        return latest_model.run_id  # Would fetch actual metric
    
    def _trigger_retraining(self):
        """Trigger retraining pipeline via Airflow or Kubeflow"""
        # In production, this would trigger an Airflow DAG
        print("🔄 Triggering model retraining pipeline...")
        # requests.post("http://airflow/api/v1/dags/eta_retraining/dagRuns", ...)

# Run drift detection periodically (would be scheduled via cron/Airflow)
if __name__ == "__main__":
    detector = ModelDriftDetector("eta-predictor")
    drifted, score = detector.detect_drift()
    if drifted:
        print(f"Drift detected! Score: {score}")
    detector.check_retraining_trigger()