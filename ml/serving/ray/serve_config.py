"""
NIDAW Ray Serve Configuration
Production-grade model serving with Ray
"""

import os
from typing import Dict, Any
from ray import serve
from ray.serve import Application
from ray.serve.drivers import DAGDriver
from ray.serve.http_adapters import json_request
import torch
import mlflow


# ============================================================================
# CONFIGURATION
# ============================================================================

RAY_ADDRESS = os.getenv("RAY_ADDRESS", "auto")
MLFLOW_TRACKING_URI = os.getenv("MLFLOW_TRACKING_URI", "http://mlflow:5000")
REDIS_URL = os.getenv("REDIS_URL", "redis://redis:6379")

# Model configurations
MODEL_CONFIGS = {
    "eta_prediction": {
        "model_name": "nidaw-eta-predictor",
        "version": "Production",
        "num_replicas": 4,
        "max_batch_size": 16,
        "batch_wait_timeout_s": 0.05,
        "resources": {"CPU": 1, "GPU": 0},
    },
    "demand_forecasting": {
        "model_name": "nidaw-demand-forecaster",
        "version": "Production",
        "num_replicas": 2,
        "max_batch_size": 8,
        "batch_wait_timeout_s": 0.1,
        "resources": {"CPU": 2, "GPU": 0},
    },
    "dynamic_pricing": {
        "model_name": "nidaw-pricing-optimizer",
        "version": "Production",
        "num_replicas": 3,
        "max_batch_size": 12,
        "batch_wait_timeout_s": 0.075,
        "resources": {"CPU": 1, "GPU": 0},
    },
    "driver_churn": {
        "model_name": "nidaw-driver-churn-predictor",
        "version": "Production",
        "num_replicas": 2,
        "max_batch_size": 32,
        "batch_wait_timeout_s": 0.2,
        "resources": {"CPU": 1, "GPU": 0},
    },
    "restaurant_recommendation": {
        "model_name": "nidaw-restaurant-recommender",
        "version": "Production",
        "num_replicas": 4,
        "max_batch_size": 16,
        "batch_wait_timeout_s": 0.05,
        "resources": {"CPU": 2, "GPU": 1},
    },
}


# ============================================================================
# MODEL DEPLOYMENT CLASS
# ============================================================================

@serve.deployment(
    num_replicas=1,
    ray_actor_options={"num_cpus": 1, "num_gpus": 0},
)
class ModelDeployment:
    """Base deployment class for ML models"""
    
    def __init__(self, model_name: str, model_version: str = "Production"):
        self.model_name = model_name
        self.model_version = model_version
        self.model = None
        self.feature_store = None
        self._load_model()
        self._init_feature_store()
    
    def _load_model(self):
        """Load model from MLflow"""
        try:
            mlflow.set_tracking_uri(MLFLOW_TRACKING_URI)
            model_uri = f"models:/{self.model_name}/{self.model_version}"
            self.model = mlflow.pyfunc.load_model(model_uri)
            print(f"✅ Loaded model: {self.model_name} (version: {self.model_version})")
        except Exception as e:
            print(f"❌ Failed to load model {self.model_name}: {e}")
            raise
    
    def _init_feature_store(self):
        """Initialize Feast feature store"""
        try:
            from feast import FeatureStore
            self.feature_store = FeatureStore(repo_path="/app/feature_store")
            print("✅ Initialized feature store")
        except Exception as e:
            print(f"⚠️ Feature store initialization failed: {e}")
            self.feature_store = None
    
    def _get_features(self, entity_ids: Dict[str, Any]) -> Dict[str, Any]:
        """Fetch features from feature store"""
        if not self.feature_store:
            return {}
        
        try:
            features = self.feature_store.get_online_features(
                features=self._get_feature_refs(),
                entity_rows=[entity_ids],
            ).to_dict()
            return features
        except Exception as e:
            print(f"⚠️ Feature fetch failed: {e}")
            return {}
    
    def _get_feature_refs(self):
        """Get feature references (override in subclass)"""
        return []
    
    async def __call__(self, request_data: Dict[str, Any]) -> Dict[str, Any]:
        """Handle inference request"""
        try:
            # Get features
            features = self._get_features(request_data.get("entity_ids", {}))
            
            # Merge with request data
            input_data = {**request_data, **features}
            
            # Run inference
            prediction = self.model.predict([input_data])
            
            return {
                "prediction": prediction[0] if len(prediction) > 0 else None,
                "model_name": self.model_name,
                "model_version": self.model_version,
                "status": "success",
            }
        except Exception as e:
            return {
                "error": str(e),
                "status": "error",
            }


# ============================================================================
# ETA PREDICTION DEPLOYMENT
# ============================================================================

@serve.deployment(
    name="eta_prediction",
    num_replicas=MODEL_CONFIGS["eta_prediction"]["num_replicas"],
    max_batch_size=MODEL_CONFIGS["eta_prediction"]["max_batch_size"],
    batch_wait_timeout_s=MODEL_CONFIGS["eta_prediction"]["batch_wait_timeout_s"],
    ray_actor_options=MODEL_CONFIGS["eta_prediction"]["resources"],
)
class ETAPredictionDeployment(ModelDeployment):
    """ETA prediction model deployment"""
    
    def __init__(self):
        super().__init__(
            model_name=MODEL_CONFIGS["eta_prediction"]["model_name"],
            model_version=MODEL_CONFIGS["eta_prediction"]["version"],
        )
    
    def _get_feature_refs(self):
        return [
            "ride_features:estimated_distance_km",
            "ride_features:estimated_duration_minutes",
            "ride_features:requested_hour",
            "ride_features:is_rush_hour",
            "ride_features:traffic_level",
            "driver_stats:avg_rating",
            "driver_stats:acceptance_rate",
        ]
    
    async def __call__(self, request_data: Dict[str, Any]) -> Dict[str, Any]:
        result = await super().__call__(request_data)
        
        # Add confidence score
        if result["status"] == "success" and result["prediction"]:
            result["confidence"] = 0.85  # TODO: Calculate actual confidence
            result["eta_minutes"] = int(result["prediction"])
        
        return result


# ============================================================================
# DEMAND FORECASTING DEPLOYMENT
# ============================================================================

@serve.deployment(
    name="demand_forecasting",
    num_replicas=MODEL_CONFIGS["demand_forecasting"]["num_replicas"],
    max_batch_size=MODEL_CONFIGS["demand_forecasting"]["max_batch_size"],
    batch_wait_timeout_s=MODEL_CONFIGS["demand_forecasting"]["batch_wait_timeout_s"],
    ray_actor_options=MODEL_CONFIGS["demand_forecasting"]["resources"],
)
class DemandForecastingDeployment(ModelDeployment):
    """Demand forecasting model deployment"""
    
    def __init__(self):
        super().__init__(
            model_name=MODEL_CONFIGS["demand_forecasting"]["model_name"],
            model_version=MODEL_CONFIGS["demand_forecasting"]["version"],
        )
    
    def _get_feature_refs(self):
        return [
            "demand_features:requests_last_hour",
            "demand_features:requests_last_24h",
            "demand_features:available_drivers",
            "demand_features:demand_supply_ratio",
        ]
    
    async def __call__(self, request_data: Dict[str, Any]) -> Dict[str, Any]:
        result = await super().__call__(request_data)
        
        if result["status"] == "success" and result["prediction"]:
            result["predicted_demand"] = int(result["prediction"])
            result["confidence"] = 0.80
        
        return result


# ============================================================================
# DYNAMIC PRICING DEPLOYMENT
# ============================================================================

@serve.deployment(
    name="dynamic_pricing",
    num_replicas=MODEL_CONFIGS["dynamic_pricing"]["num_replicas"],
    max_batch_size=MODEL_CONFIGS["dynamic_pricing"]["max_batch_size"],
    batch_wait_timeout_s=MODEL_CONFIGS["dynamic_pricing"]["batch_wait_timeout_s"],
    ray_actor_options=MODEL_CONFIGS["dynamic_pricing"]["resources"],
)
class DynamicPricingDeployment(ModelDeployment):
    """Dynamic pricing model deployment"""
    
    def __init__(self):
        super().__init__(
            model_name=MODEL_CONFIGS["dynamic_pricing"]["model_name"],
            model_version=MODEL_CONFIGS["dynamic_pricing"]["version"],
        )
    
    def _get_feature_refs(self):
        return [
            "demand_features:demand_supply_ratio",
            "demand_features:requests_last_hour",
            "ride_features:estimated_distance_km",
        ]
    
    async def __call__(self, request_data: Dict[str, Any]) -> Dict[str, Any]:
        result = await super().__call__(request_data)
        
        if result["status"] == "success" and result["prediction"]:
            result["surge_multiplier"] = float(result["prediction"])
            result["optimal_price"] = request_data.get("base_fare", 0) * result["surge_multiplier"]
        
        return result


# ============================================================================
# DRIVER CHURN DEPLOYMENT
# ============================================================================

@serve.deployment(
    name="driver_churn",
    num_replicas=MODEL_CONFIGS["driver_churn"]["num_replicas"],
    max_batch_size=MODEL_CONFIGS["driver_churn"]["max_batch_size"],
    batch_wait_timeout_s=MODEL_CONFIGS["driver_churn"]["batch_wait_timeout_s"],
    ray_actor_options=MODEL_CONFIGS["driver_churn"]["resources"],
)
class DriverChurnDeployment(ModelDeployment):
    """Driver churn prediction deployment"""
    
    def __init__(self):
        super().__init__(
            model_name=MODEL_CONFIGS["driver_churn"]["model_name"],
            model_version=MODEL_CONFIGS["driver_churn"]["version"],
        )
    
    def _get_feature_refs(self):
        return [
            "driver_stats:avg_rating",
            "driver_stats:acceptance_rate",
            "driver_stats:completion_rate",
            "driver_stats:days_since_last_ride",
            "driver_stats:earnings_last_30_days",
        ]
    
    async def __call__(self, request_data: Dict[str, Any]) -> Dict[str, Any]:
        result = await super().__call__(request_data)
        
        if result["status"] == "success" and result["prediction"]:
            churn_prob = float(result["prediction"])
            result["churn_probability"] = churn_prob
            result["risk_level"] = (
                "high" if churn_prob > 0.7 else
                "medium" if churn_prob > 0.4 else
                "low"
            )
        
        return result


# ============================================================================
# RESTAURANT RECOMMENDATION DEPLOYMENT
# ============================================================================

@serve.deployment(
    name="restaurant_recommendation",
    num_replicas=MODEL_CONFIGS["restaurant_recommendation"]["num_replicas"],
    max_batch_size=MODEL_CONFIGS["restaurant_recommendation"]["max_batch_size"],
    batch_wait_timeout_s=MODEL_CONFIGS["restaurant_recommendation"]["batch_wait_timeout_s"],
    ray_actor_options=MODEL_CONFIGS["restaurant_recommendation"]["resources"],
)
class RestaurantRecommendationDeployment(ModelDeployment):
    """Restaurant recommendation model deployment"""
    
    def __init__(self):
        super().__init__(
            model_name=MODEL_CONFIGS["restaurant_recommendation"]["model_name"],
            model_version=MODEL_CONFIGS["restaurant_recommendation"]["version"],
        )
    
    async def __call__(self, request_data: Dict[str, Any]) -> Dict[str, Any]:
        try:
            user_id = request_data.get("user_id")
            top_k = request_data.get("top_k", 10)
            
            # Get recommendations
            recommendations = self.model.recommend_for_user(
                user_id=user_id,
                top_k=top_k,
                context=request_data.get("context", {}),
            )
            
            return {
                "recommendations": recommendations,
                "status": "success",
            }
        except Exception as e:
            return {
                "error": str(e),
                "status": "error",
            }


# ============================================================================
# APPLICATION BUILDER
# ============================================================================

def app_builder() -> Application:
    """Build the Ray Serve application"""
    
    # Deploy all models
    eta_deployment = ETAPredictionDeployment.bind()
    demand_deployment = DemandForecastingDeployment.bind()
    pricing_deployment = DynamicPricingDeployment.bind()
    churn_deployment = DriverChurnDeployment.bind()
    recommendation_deployment = RestaurantRecommendationDeployment.bind()
    
    # Create DAG
    return DAGDriver.options(route_prefix="/").bind(
        {
            "eta": eta_deployment,
            "demand": demand_deployment,
            "pricing": pricing_deployment,
            "churn": churn_deployment,
            "recommendation": recommendation_deployment,
        },
        http_adapter=json_request,
    )


# ============================================================================
# ENTRY POINT
# ============================================================================

if __name__ == "__main__":
    # Start Ray Serve
    serve.run(app_builder())
    print("✅ Ray Serve application started")
    print("📊 Dashboard: http://localhost:8265")
    print("🔌 API: http://localhost:8000")