from datetime import timedelta
from feast import Entity, FeatureService, FeatureView, Field, FileSource
from feast.types import Float32, Float64, Int64, String, Bool

ride = Entity(
    name="ride_id",
    join_keys=["ride_id"],
    description="Unique ride identifier"
)

ride_request_source = FileSource(
    path="data/ride_features.parquet",
    timestamp_field="event_timestamp",
)

ride_features_fv = FeatureView(
    name="ride_features",
    entities=[ride],
    ttl=timedelta(hours=24),
    schema=[
        Field(name="pickup_lat", dtype=Float64),
        Field(name="pickup_lng", dtype=Float64),
        Field(name="dropoff_lat", dtype=Float64),
        Field(name="dropoff_lng", dtype=Float64),
        Field(name="estimated_distance_km", dtype=Float32),
        Field(name="estimated_duration_minutes", dtype=Float32),
        Field(name="requested_hour", dtype=Int64),
        Field(name="requested_day_of_week", dtype=Int64),
        Field(name="is_rush_hour", dtype=Bool),
        Field(name="weather_condition", dtype=String),
        Field(name="traffic_level", dtype=String),
        Field(name="surge_multiplier", dtype=Float32),
        Field(name="user_tier", dtype=String),
        Field(name="historical_demand_in_area", dtype=Float32),
    ],
    online=True,
    source=ride_request_source,
    tags={"team": "nidus", "domain": "ride"},
)

ride_feature_service = FeatureService(
    name="ride_features",
    features=[ride_features_fv],
)