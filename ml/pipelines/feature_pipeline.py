"""
NIDAW Feature Engineering Pipeline
Real-time feature computation and storage
"""

import os
import logging
from typing import Dict, Any, List
from datetime import datetime, timedelta
import pandas as pd
import numpy as np
from feast import FeatureStore
import redis
import json

# Configure logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


# ============================================================================
# FEATURE ENGINE
# ============================================================================

class FeatureEngine:
    """Real-time feature computation engine"""
    
    def __init__(
        self,
        feature_store_path: str = "/app/feature_store",
        redis_url: str = "redis://redis:6379",
    ):
        self.feature_store = FeatureStore(repo_path=feature_store_path)
        self.redis_client = redis.from_url(redis_url)
    
    def compute_ride_features(self, ride_data: Dict[str, Any]) -> Dict[str, Any]:
        """Compute features for ride prediction"""
        
        features = {}
        
        # Distance features
        features["estimated_distance_km"] = self._calculate_distance(
            ride_data["pickup_lat"],
            ride_data["pickup_lng"],
            ride_data["dropoff_lat"],
            ride_data["dropoff_lng"],
        )
        
        # Time features
        requested_at = datetime.fromisoformat(ride_data["requested_at"])
        features["requested_hour"] = requested_at.hour
        features["requested_day_of_week"] = requested_at.weekday()
        features["is_rush_hour"] = self._is_rush_hour(requested_at)
        features["is_weekend"] = requested_at.weekday() >= 5
        
        # Traffic features (mock - use real traffic API in production)
        features["traffic_level"] = self._estimate_traffic(requested_at)
        
        # Weather features (mock - use real weather API in production)
        features["weather_condition"] = self._get_weather(
            ride_data["pickup_lat"],
            ride_data["pickup_lng"],
        )
        
        # Historical demand
        features["historical_demand_in_area"] = self._get_historical_demand(
            ride_data["pickup_lat"],
            ride_data["pickup_lng"],
            requested_at,
        )
        
        return features
    
    def compute_driver_features(self, driver_id: str) -> Dict[str, Any]:
        """Compute features for driver"""
        
        # Get from feature store
        entity_df = pd.DataFrame([{"driver_id": driver_id}])
        
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
        ]
        
        features = self.feature_store.get_online_features(
            features=feature_refs,
            entity_rows=[{"driver_id": driver_id}],
        ).to_dict()
        
        # Flatten
        return {k: v[0] if isinstance(v, list) else v for k, v in features.items()}
    
    def compute_demand_features(self, lat: float, lng: float, timestamp: datetime) -> Dict[str, Any]:
        """Compute demand features for an area"""
        
        # Get H3 cell
        h3_cell = self._latlng_to_h3(lat, lng, resolution=8)
        
        # Get from feature store
        feature_refs = [
            "demand_features:requests_last_hour",
            "demand_features:requests_last_24h",
            "demand_features:available_drivers",
            "demand_features:demand_supply_ratio",
            "demand_features:avg_wait_time_minutes",
        ]
        
        features = self.feature_store.get_online_features(
            features=feature_refs,
            entity_rows=[{"h3_cell": h3_cell}],
        ).to_dict()
        
        return {k: v[0] if isinstance(v, list) else v for k, v in features.items()}
    
    def _calculate_distance(self, lat1: float, lng1: float, lat2: float, lng2: float) -> float:
        """Calculate distance between two points (Haversine formula)"""
        R = 6371  # Earth radius in km
        
        dlat = np.radians(lat2 - lat1)
        dlng = np.radians(lng2 - lng1)
        
        a = np.sin(dlat/2)**2 + np.cos(np.radians(lat1)) * np.cos(np.radians(lat2)) * np.sin(dlng/2)**2
        c = 2 * np.arctan2(np.sqrt(a), np.sqrt(1-a))
        
        return R * c
    
    def _is_rush_hour(self, timestamp: datetime) -> bool:
        """Check if timestamp is during rush hour"""
        hour = timestamp.hour
        return (7 <= hour <= 9) or (17 <= hour <= 19)
    
    def _estimate_traffic(self, timestamp: datetime) -> str:
        """Estimate traffic level (mock)"""
        hour = timestamp.hour
        
        if (7 <= hour <= 9) or (17 <= hour <= 19):
            return "heavy"
        elif (10 <= hour <= 16) or (20 <= hour <= 22):
            return "moderate"
        else:
            return "low"
    
    def _get_weather(self, lat: float, lng: float) -> str:
        """Get weather condition (mock - use real API)"""
        # TODO: Integrate with OpenWeatherMap or similar
        return "clear"
    
    def _get_historical_demand(self, lat: float, lng: float, timestamp: datetime) -> float:
        """Get historical demand for area (mock)"""
        # TODO: Query historical data
        return np.random.uniform(0.5, 2.0)
    
    def _latlng_to_h3(self, lat: float, lng: float, resolution: int = 8) -> str:
        """Convert lat/lng to H3 cell ID"""
        # TODO: Use h3 library
        return f"h3_{int(lat*1000)}_{int(lng*1000)}"


# ============================================================================
# FEATURE PIPELINE
# ============================================================================

class FeaturePipeline:
    """Batch feature computation pipeline"""
    
    def __init__(self, feature_engine: FeatureEngine):
        self.feature_engine = feature_engine
    
    def run_daily_update(self):
        """Run daily feature update"""
        logger.info("🚀 Starting daily feature update")
        
        try:
            # Update driver features
            self._update_driver_features()
            
            # Update demand features
            self._update_demand_features()
            
            # Update historical features
            self._update_historical_features()
            
            logger.info("✅ Daily feature update completed")
            
        except Exception as e:
            logger.error(f"❌ Daily feature update failed: {e}")
            raise
    
    def _update_driver_features(self):
        """Update driver statistics"""
        logger.info("📊 Updating driver features")
        
        # TODO: Query database for driver stats
        # TODO: Compute aggregations
        # TODO: Update feature store
        
        pass
    
    def _update_demand_features(self):
        """Update demand statistics"""
        logger.info("📊 Updating demand features")
        
        # TODO: Query ride history
        # TODO: Compute demand by H3 cell
        # TODO: Update feature store
        
        pass
    
    def _update_historical_features(self):
        """Update historical features"""
        logger.info("📊 Updating historical features")
        
        # TODO: Compute long-term trends
        # TODO: Update feature store
        
        pass


# ============================================================================
# MAIN
# ============================================================================

if __name__ == "__main__":
    # Initialize
    feature_engine = FeatureEngine()
    pipeline = FeaturePipeline(feature_engine)
    
    # Run daily update
    pipeline.run_daily_update()
    
    # Test real-time feature computation
    ride_data = {
        "pickup_lat": 40.7128,
        "pickup_lng": -74.0060,
        "dropoff_lat": 40.7589,
        "dropoff_lng": -73.9851,
        "requested_at": datetime.utcnow().isoformat(),
    }
    
    features = feature_engine.compute_ride_features(ride_data)
    print(json.dumps(features, indent=2))