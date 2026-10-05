import os
import mlflow
import numpy as np
import pandas as pd
from sklearn.ensemble import GradientBoostingRegressor
import joblib

class DynamicPricingOptimizer:
    """
    Multi-objective pricing optimizer that balances:
    1. Rider conversion rate
    2. Driver earnings
    3. Platform revenue
    4. Market equilibrium
    """
    
    def __init__(self):
        self.conversion_model = None
        self.demand_model = None
        
    def train_conversion_model(self, df: pd.DataFrame):
        """Train model to predict ride acceptance probability at different prices"""
        features = [
            'base_fare', 'surge_multiplier', 'estimated_distance_km',
            'estimated_duration_minutes', 'hour_of_day', 'day_of_week',
            'demand_supply_ratio', 'user_tier', 'historical_avg_fare',
            'competitor_price', 'is_rush_hour', 'weather_encoded'
        ]
        
        X = df[features].fillna(0)
        y = df['accepted']  # Binary: 1 if rider accepted, 0 if cancelled
        
        model = GradientBoostingRegressor(
            n_estimators=300,
            max_depth=6,
            learning_rate=0.05,
            subsample=0.8,
        )
        model.fit(X, y)
        
        self.conversion_model = model
        mlflow.sklearn.log_model(model, "conversion_model")
        return model
    
    def optimize_price(self, ride_request: dict) -> dict:
        """
        Find optimal price using grid search over surge multipliers
        """
        base_fare = ride_request['base_fare']
        
        # Test different surge multipliers
        surge_candidates = np.arange(1.0, 3.0, 0.1)
        best_revenue = 0
        best_surge = 1.0
        
        for surge in surge_candidates:
            price = base_fare * surge
            
            # Predict conversion probability
            features = self._prepare_features(ride_request, surge)
            conversion_prob = self.conversion_model.predict([features])[0]
            conversion_prob = np.clip(conversion_prob, 0, 1)
            
            # Calculate expected revenue
            platform_fee_rate = 0.20
            expected_revenue = price * platform_fee_rate * conversion_prob
            
            # Penalize extreme surges (user experience)
            if surge > 2.0:
                expected_revenue *= 0.8
            
            if expected_revenue > best_revenue:
                best_revenue = expected_revenue
                best_surge = surge
        
        return {
            'optimal_surge': best_surge,
            'final_price': base_fare * best_surge,
            'expected_revenue': best_revenue,
            'conversion_probability': conversion_prob,
        }
    
    def _prepare_features(self, request, surge):
        # Prepare feature vector for model
        return [
            request['base_fare'],
            surge,
            request['estimated_distance_km'],
            request['estimated_duration_minutes'],
            request['hour_of_day'],
            request['day_of_week'],
            request['demand_supply_ratio'],
            request.get('user_tier', 'standard'),
            request['historical_avg_fare'],
            request.get('competitor_price', 0),
            request['is_rush_hour'],
            request.get('weather_encoded', 0),
        ]