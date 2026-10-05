"""
NIDAW Model Monitoring & Metrics Collection
Track model performance, drift, and business metrics
"""

import os
import time
import json
import logging
from typing import Dict, Any, Optional, List
from datetime import datetime, timedelta
from dataclasses import dataclass, asdict
import numpy as np
import pandas as pd
import mlflow
import redis
from prometheus_client import Counter, Histogram, Gauge, start_http_server

# Configure logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


# ============================================================================
# PROMETHEUS METRICS
# ============================================================================

# Counters
INFERENCE_REQUESTS = Counter(
    'ml_inference_requests_total',
    'Total inference requests',
    ['model_name', 'status']
)

INFERENCE_ERRORS = Counter(
    'ml_inference_errors_total',
    'Total inference errors',
    ['model_name', 'error_type']
)

# Histograms
INFERENCE_LATENCY = Histogram(
    'ml_inference_latency_seconds',
    'Inference latency in seconds',
    ['model_name'],
    buckets=[0.01, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0]
)

# Gauges
MODEL_VERSION = Gauge(
    'ml_model_version',
    'Current model version',
    ['model_name']
)

ACTIVE_MODELS = Gauge(
    'ml_active_models',
    'Number of active models'
)


# ============================================================================
# DATA CLASSES
# ============================================================================

@dataclass
class InferenceMetrics:
    """Metrics for a single inference request"""
    model_name: str
    model_version: str
    request_id: str
    timestamp: datetime
    latency_ms: float
    status: str
    prediction: Any
    confidence: Optional[float] = None
    features: Optional[Dict[str, Any]] = None
    error: Optional[str] = None


@dataclass
class ModelDriftMetrics:
    """Model drift detection metrics"""
    model_name: str
    timestamp: datetime
    feature_drift_score: float
    prediction_drift_score: float
    data_quality_score: float
    drifted_features: List[str]
    severity: str  # low, medium, high, critical


@dataclass
class BusinessMetrics:
    """Business impact metrics"""
    model_name: str
    timestamp: datetime
    predictions_made: int
    accuracy: Optional[float] = None
    revenue_impact: Optional[float] = None
    user_satisfaction: Optional[float] = None


# ============================================================================
# METRICS COLLECTOR
# ============================================================================

class ModelMetricsCollector:
    """Collects and stores model metrics"""
    
    def __init__(
        self,
        redis_url: str = "redis://redis:6379",
        mlflow_uri: str = "http://mlflow:5000",
        prometheus_port: int = 8000,
    ):
        self.redis_client = redis.from_url(redis_url)
        self.mlflow_uri = mlflow_uri
        self.metrics_buffer: List[InferenceMetrics] = []
        self.buffer_size = 100
        
        # Start Prometheus server
        start_http_server(prometheus_port)
        logger.info(f"✅ Prometheus metrics server started on port {prometheus_port}")
    
    def record_inference(
        self,
        model_name: str,
        model_version: str,
        request_id: str,
        latency_ms: float,
        status: str,
        prediction: Any,
        confidence: Optional[float] = None,
        features: Optional[Dict[str, Any]] = None,
        error: Optional[str] = None,
    ) -> InferenceMetrics:
        """Record inference metrics"""
        
        metrics = InferenceMetrics(
            model_name=model_name,
            model_version=model_version,
            request_id=request_id,
            timestamp=datetime.utcnow(),
            latency_ms=latency_ms,
            status=status,
            prediction=prediction,
            confidence=confidence,
            features=features,
            error=error,
        )
        
        # Update Prometheus metrics
        INFERENCE_REQUESTS.labels(model_name=model_name, status=status).inc()
        INFERENCE_LATENCY.labels(model_name=model_name).observe(latency_ms / 1000)
        
        if error:
            INFERENCE_ERRORS.labels(model_name=model_name, error_type=error).inc()
        
        # Add to buffer
        self.metrics_buffer.append(metrics)
        
        # Flush if buffer is full
        if len(self.metrics_buffer) >= self.buffer_size:
            self._flush_metrics()
        
        # Store in Redis for real-time monitoring
        self._store_realtime_metrics(metrics)
        
        return metrics
    
    def _flush_metrics(self):
        """Flush metrics buffer to storage"""
        if not self.metrics_buffer:
            return
        
        try:
            # Convert to DataFrame
            df = pd.DataFrame([asdict(m) for m in self.metrics_buffer])
            
            # Store in MLflow
            with mlflow.start_run(run_name=f"metrics_{int(time.time())}"):
                mlflow.log_metrics({
                    "total_requests": len(df),
                    "avg_latency_ms": df["latency_ms"].mean(),
                    "p95_latency_ms": df["latency_ms"].quantile(0.95),
                    "error_rate": (df["status"] == "error").mean(),
                })
            
            logger.info(f"✅ Flushed {len(self.metrics_buffer)} metrics to MLflow")
            self.metrics_buffer.clear()
            
        except Exception as e:
            logger.error(f"❌ Failed to flush metrics: {e}")
    
    def _store_realtime_metrics(self, metrics: InferenceMetrics):
        """Store metrics in Redis for real-time monitoring"""
        try:
            key = f"ml:metrics:{metrics.model_name}:{metrics.timestamp.strftime('%Y%m%d%H%M')}"
            self.redis_client.hset(key, mapping={
                "request_id": metrics.request_id,
                "latency_ms": metrics.latency_ms,
                "status": metrics.status,
                "prediction": json.dumps(metrics.prediction),
            })
            self.redis_client.expire(key, 3600)  # 1 hour TTL
        except Exception as e:
            logger.error(f"❌ Failed to store realtime metrics: {e}")
    
    def get_realtime_metrics(self, model_name: str, minutes: int = 5) -> Dict[str, Any]:
        """Get real-time metrics for a model"""
        try:
            pattern = f"ml:metrics:{model_name}:*"
            keys = self.redis_client.keys(pattern)
            
            if not keys:
                return {"error": "No metrics found"}
            
            # Aggregate metrics
            latencies = []
            statuses = []
            
            for key in keys:
                data = self.redis_client.hgetall(key)
                if data:
                    latencies.append(float(data.get("latency_ms", 0)))
                    statuses.append(data.get("status", "unknown"))
            
            return {
                "model_name": model_name,
                "total_requests": len(latencies),
                "avg_latency_ms": np.mean(latencies) if latencies else 0,
                "p95_latency_ms": np.percentile(latencies, 95) if latencies else 0,
                "error_rate": statuses.count("error") / len(statuses) if statuses else 0,
                "success_rate": statuses.count("success") / len(statuses) if statuses else 0,
            }
        except Exception as e:
            logger.error(f"❌ Failed to get realtime metrics: {e}")
            return {"error": str(e)}


# ============================================================================
# DRIFT DETECTOR
# ============================================================================

class ModelDriftDetector:
    """Detects model and data drift"""
    
    def __init__(self, redis_url: str = "redis://redis:6379"):
        self.redis_client = redis.from_url(redis_url)
    
    def detect_drift(
        self,
        model_name: str,
        reference_data: pd.DataFrame,
        current_data: pd.DataFrame,
        threshold: float = 0.1,
    ) -> ModelDriftMetrics:
        """Detect drift between reference and current data"""
        
        try:
            # Calculate feature drift
            feature_drift_scores = {}
            drifted_features = []
            
            for column in reference_data.columns:
                if column in current_data.columns:
                    # Calculate distribution difference (KS test or PSI)
                    ref_dist = reference_data[column].values
                    curr_dist = current_data[column].values
                    
                    # Simple drift score (can be replaced with statistical test)
                    drift_score = self._calculate_drift_score(ref_dist, curr_dist)
                    feature_drift_scores[column] = drift_score
                    
                    if drift_score > threshold:
                        drifted_features.append(column)
            
            # Calculate overall drift scores
            feature_drift = np.mean(list(feature_drift_scores.values()))
            prediction_drift = self._calculate_prediction_drift(reference_data, current_data)
            data_quality = self._calculate_data_quality(current_data)
            
            # Determine severity
            max_drift = max(feature_drift, prediction_drift)
            severity = (
                "critical" if max_drift > 0.5 else
                "high" if max_drift > 0.3 else
                "medium" if max_drift > 0.15 else
                "low"
            )
            
            metrics = ModelDriftMetrics(
                model_name=model_name,
                timestamp=datetime.utcnow(),
                feature_drift_score=feature_drift,
                prediction_drift_score=prediction_drift,
                data_quality_score=data_quality,
                drifted_features=drifted_features,
                severity=severity,
            )
            
            # Store in Redis
            self._store_drift_metrics(metrics)
            
            # Alert if severe
            if severity in ["high", "critical"]:
                self._send_alert(metrics)
            
            return metrics
            
        except Exception as e:
            logger.error(f"❌ Drift detection failed: {e}")
            raise
    
    def _calculate_drift_score(self, ref_dist: np.ndarray, curr_dist: np.ndarray) -> float:
        """Calculate drift score between two distributions"""
        # Simplified - use Population Stability Index (PSI) or KS test in production
        ref_mean = np.mean(ref_dist)
        curr_mean = np.mean(curr_dist)
        ref_std = np.std(ref_dist)
        
        if ref_std == 0:
            return 0.0
        
        return abs(curr_mean - ref_mean) / ref_std
    
    def _calculate_prediction_drift(self, ref_data: pd.DataFrame, curr_data: pd.DataFrame) -> float:
        """Calculate prediction drift"""
        if "prediction" not in ref_data.columns or "prediction" not in curr_data.columns:
            return 0.0
        
        return self._calculate_drift_score(
            ref_data["prediction"].values,
            curr_data["prediction"].values,
        )
    
    def _calculate_data_quality(self, data: pd.DataFrame) -> float:
        """Calculate data quality score"""
        # Check for nulls, outliers, etc.
        null_rate = data.isnull().mean().mean()
        return 1.0 - null_rate
    
    def _store_drift_metrics(self, metrics: ModelDriftMetrics):
        """Store drift metrics in Redis"""
        try:
            key = f"ml:drift:{metrics.model_name}:{metrics.timestamp.strftime('%Y%m%d')}"
            self.redis_client.hset(key, mapping={
                "feature_drift": metrics.feature_drift_score,
                "prediction_drift": metrics.prediction_drift_score,
                "data_quality": metrics.data_quality_score,
                "severity": metrics.severity,
                "drifted_features": json.dumps(metrics.drifted_features),
            })
            self.redis_client.expire(key, 7 * 24 * 3600)  # 7 days TTL
        except Exception as e:
            logger.error(f"❌ Failed to store drift metrics: {e}")
    
    def _send_alert(self, metrics: ModelDriftMetrics):
        """Send alert for severe drift"""
        # TODO: Integrate with PagerDuty, Slack, etc.
        logger.warning(
            f"🚨 DRIFT ALERT: {metrics.model_name} - "
            f"Severity: {metrics.severity}, "
            f"Feature drift: {metrics.feature_drift_score:.3f}, "
            f"Drifted features: {metrics.drifted_features}"
        )


# ============================================================================
# MAIN
# ============================================================================

if __name__ == "__main__":
    # Example usage
    collector = ModelMetricsCollector()
    
    # Record some metrics
    for i in range(10):
        collector.record_inference(
            model_name="eta_prediction",
            model_version="v1.0.0",
            request_id=f"req_{i}",
            latency_ms=np.random.uniform(10, 100),
            status="success" if np.random.random() > 0.1 else "error",
            prediction=np.random.randint(5, 30),
        )
    
    # Get realtime metrics
    metrics = collector.get_realtime_metrics("eta_prediction")
    print(json.dumps(metrics, indent=2))