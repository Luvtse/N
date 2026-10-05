import os
import mlflow
import pandas as pd
import numpy as np
from sklearn.ensemble import GradientBoostingClassifier
from sklearn.model_selection import train_test_split, StratifiedKFold
from sklearn.metrics import (
    roc_auc_score, precision_recall_curve, average_precision_score,
    classification_report, confusion_matrix
)
from sklearn.preprocessing import StandardScaler
from imblearn.over_sampling import SMOTE
import joblib
from feast import FeatureStore

class DriverChurnPredictor:
    """
    Predicts driver churn risk (drivers likely to stop driving in next 30 days)
    Enables proactive retention campaigns
    """
    
    def __init__(self):
        self.feature_store = FeatureStore(repo_path="feature_store/feast/feature_repo")
        self.model = None
        self.scaler = StandardScaler()
        self.feature_names = None
        
    def load_training_data(self) -> pd.DataFrame:
        """Load driver activity data with churn labels"""
        # Historical driver data
        entity_df = pd.read_parquet("data/driver_activity_history.parquet")
        
        # Fetch features from feature store
        feature_refs = [
            "driver_stats:avg_rating",
            "driver_stats:total_rides",
            "driver_stats:acceptance_rate",
            "driver_stats:cancellation_rate",
            "driver_stats:avg_earnings_per_hour",
            "driver_stats:completion_rate",
            "driver_stats:avg_response_time_seconds",
            "driver_stats:days_since_last_ride",
            "driver_stats:rides_last_7_days",
            "driver_stats:rides_last_30_days",
            "driver_stats:earnings_last_7_days",
            "driver_stats:earnings_last_30_days",
            "driver_stats:online_hours_last_7_days",
            "driver_stats:peak_hour_utilization",
            "driver_stats:complaints_count",
            "driver_stats:days_as_driver",
        ]
        
        features_df = self.feature_store.get_historical_features(
            entity_df=entity_df,
            features=feature_refs,
        ).to_df()
        
        return features_df
    
    def preprocess(self, df: pd.DataFrame) -> tuple:
        """Feature engineering for churn prediction"""
        # Create engagement features
        df['engagement_score'] = (
            df['rides_last_7_days'] * 0.4 +
            df['acceptance_rate'] * 0.3 +
            df['completion_rate'] * 0.3
        )
        
        df['earnings_trend'] = (
            df['earnings_last_7_days'] / (df['earnings_last_30_days'] / 4 + 1e-6)
        )
        
        df['activity_trend'] = (
            df['rides_last_7_days'] / (df['rides_last_30_days'] / 4 + 1e-6)
        )
        
        df['satisfaction_score'] = (
            df['avg_rating'] * 20 +
            (100 - df['cancellation_rate']) * 0.3 +
            (100 - df['complaints_count'] * 10) * 0.2
        )
        
        # Handle missing values
        df = df.fillna(0)
        
        # Feature selection
        feature_columns = [
            'avg_rating', 'total_rides', 'acceptance_rate', 'cancellation_rate',
            'avg_earnings_per_hour', 'completion_rate', 'avg_response_time_seconds',
            'days_since_last_ride', 'rides_last_7_days', 'rides_last_30_days',
            'earnings_last_7_days', 'earnings_last_30_days', 'online_hours_last_7_days',
            'peak_hour_utilization', 'complaints_count', 'days_as_driver',
            'engagement_score', 'earnings_trend', 'activity_trend', 'satisfaction_score'
        ]
        
        self.feature_names = feature_columns
        X = df[feature_columns]
        y = df['churned']  # Binary: 1 if churned in last 30 days
        
        # Scale features
        X_scaled = self.scaler.fit_transform(X)
        
        return train_test_split(
            X_scaled, y, test_size=0.2, random_state=42, stratify=y
        )
    
    def train(self, X_train, X_test, y_train, y_test):
        """Train churn prediction model with class imbalance handling"""
        with mlflow.start_run(run_name="driver_churn_v1"):
            # Handle class imbalance with SMOTE
            smote = SMOTE(random_state=42, sampling_strategy=0.5)
            X_train_resampled, y_train_resampled = smote.fit_resample(X_train, y_train)
            
            # Model parameters
            params = {
                'n_estimators': 300,
                'max_depth': 6,
                'learning_rate': 0.05,
                'subsample': 0.8,
                'min_samples_split': 20,
                'min_samples_leaf': 10,
                'max_features': 'sqrt',
                'random_state': 42,
            }
            
            mlflow.log_params(params)
            mlflow.log_metric("train_samples", len(X_train_resampled))
            mlflow.log_metric("churn_rate", y_train.mean())
            
            # Train model
            model = GradientBoostingClassifier(**params)
            
            # Cross-validation
            cv = StratifiedKFold(n_splits=5, shuffle=True, random_state=42)
            cv_scores = []
            
            for train_idx, val_idx in cv.split(X_train_resampled, y_train_resampled):
                model.fit(X_train_resampled[train_idx], y_train_resampled[train_idx])
                val_pred = model.predict_proba(X_train_resampled[val_idx])[:, 1]
                cv_scores.append(roc_auc_score(y_train_resampled[val_idx], val_pred))
            
            mean_cv_score = np.mean(cv_scores)
            mlflow.log_metric("cv_auc_mean", mean_cv_score)
            mlflow.log_metric("cv_auc_std", np.std(cv_scores))
            
            # Final training on full training set
            model.fit(X_train_resampled, y_train_resampled)
            
            # Evaluate on test set
            y_pred_proba = model.predict_proba(X_test)[:, 1]
            y_pred = model.predict(X_test)
            
            # Metrics
            auc = roc_auc_score(y_test, y_pred_proba)
            avg_precision = average_precision_score(y_test, y_pred_proba)
            
            mlflow.log_metrics({
                "test_auc": auc,
                "test_avg_precision": avg_precision,
            })
            
            # Find optimal threshold
            precision, recall, thresholds = precision_recall_curve(y_test, y_pred_proba)
            f1_scores = 2 * (precision * recall) / (precision + recall + 1e-8)
            optimal_threshold = thresholds[np.argmax(f1_scores)]
            
            mlflow.log_metric("optimal_threshold", optimal_threshold)
            
            # Feature importance
            importance_df = pd.DataFrame({
                'feature': self.feature_names,
                'importance': model.feature_importances_,
            }).sort_values('importance', ascending=False)
            
            mlflow.log_table(
                data=importance_df.to_dict(orient='list'),
                artifact_file="feature_importance.json"
            )
            
            # Confusion matrix
            cm = confusion_matrix(y_test, y_pred)
            mlflow.log_dict({
                "confusion_matrix": cm.tolist(),
                "classification_report": classification_report(y_test, y_pred, output_dict=True),
            }, "evaluation_metrics.json")
            
            # Log model
            mlflow.sklearn.log_model(
                model,
                "churn_model",
                registered_model_name="nidaw-driver-churn-predictor",
                input_example=X_test[:5],
            )
            
            # Save scaler and feature names
            joblib.dump(self.scaler, "/tmp/churn_scaler.pkl")
            joblib.dump(self.feature_names, "/tmp/churn_features.pkl")
            mlflow.log_artifact("/tmp/churn_scaler.pkl")
            mlflow.log_artifact("/tmp/churn_features.pkl")
            
            print(f"✅ Churn model trained!")
            print(f"AUC: {auc:.4f}")
            print(f"Average Precision: {avg_precision:.4f}")
            print(f"Optimal threshold: {optimal_threshold:.4f}")
            
            self.model = model
            return model
    
    def predict_churn_risk(self, driver_features: dict) -> dict:
        """Predict churn risk for a driver"""
        features = pd.DataFrame([driver_features])[self.feature_names]
        features_scaled = self.scaler.transform(features)
        
        churn_prob = self.model.predict_proba(features_scaled)[0, 1]
        
        risk_level = "low"
        if churn_prob > 0.7:
            risk_level = "high"
        elif churn_prob > 0.4:
            risk_level = "medium"
        
        return {
            "churn_probability": float(churn_prob),
            "risk_level": risk_level,
            "recommended_action": self._get_recommendation(churn_prob, driver_features),
        }
    
    def _get_recommendation(self, churn_prob: float, features: dict) -> str:
        """Generate retention recommendation based on churn drivers"""
        if churn_prob < 0.3:
            return "No action needed"
        
        if features.get('earnings_trend', 1) < 0.7:
            return "Offer earnings bonus or incentive program"
        
        if features.get('avg_rating', 5) < 4.0:
            return "Provide driver coaching and support"
        
        if features.get('days_since_last_ride', 0) > 14:
            return "Send re-engagement campaign with welcome-back bonus"
        
        if features.get('cancellation_rate', 0) > 20:
            return "Investigate cancellation reasons, offer support"
        
        return "Personalized outreach from driver success team"

if __name__ == "__main__":
    predictor = DriverChurnPredictor()
    df = predictor.load_training_data()
    X_train, X_test, y_train, y_test = predictor.preprocess(df)
    predictor.train(X_train, X_test, y_train, y_test)