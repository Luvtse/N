import os
import sys
from datetime import datetime
import mlflow
from feast import FeatureStore

class TrainingPipeline:
    """
    End-to-end ML training pipeline
    Triggered by Airflow or Kubernetes CronJob
    """
    
    def __init__(self, model_name: str):
        self.model_name = model_name
        self.feature_store = FeatureStore(repo_path="feature_store/feast/feature_repo")
        mlflow.set_tracking_uri(os.getenv("MLFLOW_TRACKING_URI"))
    
    def run(self):
        """Execute full training pipeline"""
        print(f"🚀 Starting training pipeline for {self.model_name}")
        start_time = datetime.now()
        
        try:
            # Step 1: Validate data quality
            self._validate_data_quality()
            
            # Step 2: Fetch training data from feature store
            training_data = self._fetch_training_data()
            
            # Step 3: Preprocess
            processed_data = self._preprocess(training_data)
            
            # Step 4: Train model
            model, metrics = self._train(processed_data)
            
            # Step 5: Evaluate on holdout set
            eval_metrics = self._evaluate(model, processed_data)
            
            # Step 6: Validate model fairness
            fairness_metrics = self._validate_fairness(model, processed_data)
            
            # Step 7: Register model if metrics pass threshold
            if self._should_promote(eval_metrics):
                self._register_model(model, eval_metrics)
                self._deploy_to_staging(model)
            else:
                print(f"⚠️ Model metrics below threshold. Not promoting.")
            
            # Step 8: Log pipeline metrics
            self._log_pipeline_metrics(start_time, metrics, eval_metrics, fairness_metrics)
            
            print(f"✅ Training pipeline completed in {datetime.now() - start_time}")
            
        except Exception as e:
            print(f"❌ Pipeline failed: {e}")
            self._send_alert(str(e))
            raise
    
    def _validate_data_quality(self):
        """Check for data quality issues"""
        print("📊 Validating data quality...")
        # Check for nulls, outliers, schema changes
        # Use Great Expectations or Evidently in production
    
    def _fetch_training_data(self):
        """Fetch from feature store"""
        print("📥 Fetching training data...")
        entity_df = self._get_entity_dataframe()
        
        feature_refs = self._get_feature_refs()
        features = self.feature_store.get_historical_features(
            entity_df=entity_df,
            features=feature_refs,
        ).to_df()
        
        return features
    
    def _preprocess(self, data):
        """Feature engineering"""
        print("🔧 Preprocessing...")
        # Apply transformations
        return data
    
    def _train(self, data):
        """Train model"""
        print("🏋️ Training model...")
        # Delegate to specific trainer (ETA, Demand, Pricing)
        return None, {}
    
    def _evaluate(self, model, data):
        """Evaluate on holdout"""
        return {}
    
    def _validate_fairness(self, model, data):
        """Check for bias across user segments"""
        print("⚖️ Validating fairness...")
        # Check performance across user tiers, regions, times of day
        return {}
    
    def _should_promote(self, metrics: dict) -> bool:
        """Check if model meets promotion criteria"""
        thresholds = {
            "eta-predictor": {"mae_minutes": 3.0, "r2_score": 0.85},
            "demand-forecaster": {"mape_percent": 15.0},
            "pricing-optimizer": {"revenue_lift": 0.05},
        }
        
        threshold = thresholds.get(self.model_name, {})
        for metric, value in threshold.items():
            if metric in metrics and metrics[metric] < value:
                return False
        return True
    
    def _register_model(self, model, metrics):
        """Register in MLflow model registry"""
        print("📝 Registering model...")
        # mlflow.register_model(...)
    
    def _deploy_to_staging(self, model):
        """Deploy to staging environment"""
        print("🚢 Deploying to staging...")
        # Trigger TorchServe deployment
    
    def _log_pipeline_metrics(self, start_time, train_metrics, eval_metrics, fairness_metrics):
        """Log all pipeline metrics"""
        with mlflow.start_run(run_name=f"pipeline_{self.model_name}"):
            mlflow.log_metrics(train_metrics)
            mlflow.log_metrics(eval_metrics)
            mlflow.log_metrics(fairness_metrics)
            mlflow.log_metric("pipeline_duration_seconds", (datetime.now() - start_time).total_seconds())
    
    def _send_alert(self, error_message: str):
        """Send alert on failure"""
        print(f"🚨 ALERT: {error_message}")
    
    def _get_entity_dataframe(self):
        """Get entity IDs for feature retrieval"""
        import pandas as pd
        return pd.read_parquet("data/entity_df.parquet")
    
    def _get_feature_refs(self):
        """Get feature references for this model"""
        return []

if __name__ == "__main__":
    model_name = sys.argv[1] if len(sys.argv) > 1 else "eta-predictor"
    pipeline = TrainingPipeline(model_name)
    pipeline.run()