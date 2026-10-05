import os
import json
import torch
import xgboost as xgb
import numpy as np
import joblib
from ts.torch_handler.base_handler import BaseHandler
from feast import FeatureStore

class ETAHandler(BaseHandler):
    def __init__(self):
        super().__init__()
        self.model = None
        self.feature_names = None
        self.feature_store = None
        self.initialized = False
    
    def initialize(self, context):
        """Load model and feature store"""
        properties = context.system_properties
        model_dir = properties.get("model_dir")
        
        # Load XGBoost model
        self.model = xgb.Booster()
        self.model.load_model(os.path.join(model_dir, "eta_model.xgb"))
        
        # Load feature names
        self.feature_names = joblib.load(os.path.join(model_dir, "feature_names.pkl"))
        
        # Initialize Feast feature store
        self.feature_store = FeatureStore(repo_path="/app/feature_store/feast/feature_repo")
        
        self.initialized = True
        print("✅ ETA Handler initialized")
    
    def preprocess(self, data):
        """Fetch features from store and transform"""
        requests = []
        for row in data:
            body = json.loads(row.get("body", b"{}"))
            ride_id = body.get("ride_id")
            driver_id = body.get("driver_id")
            
            # Fetch real-time features from online store
            feature_refs = [
                "ride_features:estimated_distance_km",
                "ride_features:estimated_duration_minutes",
                "ride_features:requested_hour",
                "ride_features:is_rush_hour",
                "ride_features:traffic_level",
                "ride_features:surge_multiplier",
                "driver_stats:avg_rating",
                "driver_stats:acceptance_rate",
            ]
            
            entity_df = pd.DataFrame([{
                "ride_id": ride_id,
                "driver_id": driver_id,
                "event_timestamp": pd.Timestamp.now(),
            }])
            
            features = self.feature_store.get_online_features(
                features=feature_refs,
                entity_rows=[{"ride_id": ride_id, "driver_id": driver_id}],
            ).to_dict()
            
            # Build feature vector
            feature_vector = [features.get(name, 0) for name in self.feature_names]
            requests.append(feature_vector)
        
        return np.array(requests)
    
    def inference(self, data):
        """Run model inference"""
        dmatrix = xgb.DMatrix(data, feature_names=self.feature_names)
        predictions = self.model.predict(dmatrix)
        return predictions
    
    def postprocess(self, data):
        """Format response"""
        results = []
        for pred in data:
            results.append({
                "eta_minutes": float(pred),
                "confidence": 0.85,
                "timestamp": pd.Timestamp.now().isoformat(),
            })
        return json.dumps(results)