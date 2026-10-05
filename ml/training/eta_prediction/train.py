import os
import mlflow
import mlflow.xgboost
import pandas as pd
import numpy as np
import xgboost as xgb
from sklearn.model_selection import train_test_split
from sklearn.metrics import mean_absolute_error, mean_squared_error, r2_score
import joblib
from feast import FeatureStore
import pyarrow.parquet as pq

# Initialize MLflow
mlflow.set_tracking_uri(os.getenv("MLFLOW_TRACKING_URI", "http://localhost:5000"))
mlflow.set_experiment("eta-prediction")

class ETAPredictorTrainer:
    def __init__(self):
        self.feature_store = FeatureStore(repo_path="feature_store/feast/feature_repo")
        self.model = None
        self.feature_names = None
        
    def load_training_data(self, start_date: str, end_date: str) -> pd.DataFrame:
        """Load historical ride data with features from offline store"""
        # Get entity dataframe (historical rides)
        entity_df = pd.read_parquet("data/historical_rides.parquet")
        
        # Fetch features from feature store
        feature_refs = [
            "ride_features:estimated_distance_km",
            "ride_features:estimated_duration_minutes",
            "ride_features:requested_hour",
            "ride_features:requested_day_of_week",
            "ride_features:is_rush_hour",
            "ride_features:weather_condition",
            "ride_features:traffic_level",
            "ride_features:surge_multiplier",
            "ride_features:historical_demand_in_area",
            "driver_stats:avg_rating",
            "driver_stats:acceptance_rate",
            "driver_stats:avg_response_time_seconds",
        ]
        
        features_df = self.feature_store.get_historical_features(
            entity_df=entity_df,
            features=feature_refs,
        ).to_df()
        
        # Add target variable (actual duration)
        # In production, this comes from completed rides
        features_df['actual_duration_minutes'] = features_df['estimated_duration_minutes'] * np.random.uniform(0.8, 1.3, len(features_df))
        
        return features_df
    
    def preprocess(self, df: pd.DataFrame) -> tuple:
        """Feature engineering and preprocessing"""
        # Handle categorical variables
        df['weather_encoded'] = df['weather_condition'].map({
            'clear': 0, 'cloudy': 1, 'rain': 2, 'snow': 3, 'storm': 4
        }).fillna(0)
        
        df['traffic_encoded'] = df['traffic_level'].map({
            'low': 0, 'moderate': 1, 'heavy': 2, 'severe': 3
        }).fillna(0)
        
        # Cyclical encoding for time features
        df['hour_sin'] = np.sin(2 * np.pi * df['requested_hour'] / 24)
        df['hour_cos'] = np.cos(2 * np.pi * df['requested_hour'] / 24)
        df['day_sin'] = np.sin(2 * np.pi * df['requested_day_of_week'] / 7)
        df['day_cos'] = np.cos(2 * np.pi * df['requested_day_of_week'] / 7)
        
        # Interaction features
        df['distance_traffic_interaction'] = df['estimated_distance_km'] * df['traffic_encoded']
        df['demand_hour_interaction'] = df['historical_demand_in_area'] * df['hour_sin']
        
        # Select features for model
        feature_columns = [
            'estimated_distance_km',
            'estimated_duration_minutes',
            'requested_hour',
            'requested_day_of_week',
            'is_rush_hour',
            'weather_encoded',
            'traffic_encoded',
            'surge_multiplier',
            'historical_demand_in_area',
            'avg_rating',
            'acceptance_rate',
            'avg_response_time_seconds',
            'hour_sin',
            'hour_cos',
            'day_sin',
            'day_cos',
            'distance_traffic_interaction',
            'demand_hour_interaction',
        ]
        
        self.feature_names = feature_columns
        X = df[feature_columns].fillna(0)
        y = df['actual_duration_minutes']
        
        return train_test_split(X, y, test_size=0.2, random_state=42)
    
    def train(self, X_train, X_test, y_train, y_test):
        """Train XGBoost model with hyperparameter tuning"""
        with mlflow.start_run(run_name="eta_xgboost_v1"):
            # Log parameters
            params = {
                'n_estimators': 500,
                'max_depth': 8,
                'learning_rate': 0.05,
                'subsample': 0.8,
                'colsample_bytree': 0.8,
                'min_child_weight': 5,
                'gamma': 0.1,
                'reg_alpha': 0.1,
                'reg_lambda': 1.0,
                'objective': 'reg:squarederror',
                'eval_metric': 'mae',
                'tree_method': 'hist',
                'random_state': 42,
            }
            
            mlflow.log_params(params)
            
            # Train model
            model = xgb.XGBRegressor(**params)
            model.fit(
                X_train, y_train,
                eval_set=[(X_test, y_test)],
                early_stopping_rounds=50,
                verbose=100,
            )
            
            # Evaluate
            y_pred = model.predict(X_test)
            
            mae = mean_absolute_error(y_test, y_pred)
            rmse = np.sqrt(mean_squared_error(y_test, y_pred))
            r2 = r2_score(y_test, y_pred)
            mape = np.mean(np.abs((y_test - y_pred) / np.maximum(y_test, 1e-8))) * 100
            
            # Log metrics
            mlflow.log_metrics({
                "mae_minutes": mae,
                "rmse_minutes": rmse,
                "r2_score": r2,
                "mape_percent": mape,
            })
            
            # Log feature importance
            importance_df = pd.DataFrame({
                'feature': self.feature_names,
                'importance': model.feature_importances_,
            }).sort_values('importance', ascending=False)
            
            mlflow.log_table(
                data=importance_df.to_dict(orient='list'),
                artifact_file="feature_importance.json"
            )
            
            # Log model
            mlflow.xgboost.log_model(
                model,
                "eta_model",
                registered_model_name="nidaw-eta-predictor",
                input_example=X_test.iloc[:5],
            )
            
            # Save feature names for inference
            joblib.dump(self.feature_names, "/tmp/feature_names.pkl")
            mlflow.log_artifact("/tmp/feature_names.pkl")
            
            print(f"✅ Model trained successfully!")
            print(f"MAE: {mae:.2f} minutes")
            print(f"RMSE: {rmse:.2f} minutes")
            print(f"R²: {r2:.4f}")
            print(f"MAPE: {mape:.2f}%")
            
            self.model = model
            return model

if __name__ == "__main__":
    trainer = ETAPredictorTrainer()
    df = trainer.load_training_data("2026-01-01", "2026-07-01")
    X_train, X_test, y_train, y_test = trainer.preprocess(df)
    trainer.train(X_train, X_test, y_train, y_test)