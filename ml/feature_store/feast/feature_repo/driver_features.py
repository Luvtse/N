from datetime import timedelta
from feast import Entity, FeatureService, FeatureView, Field, FileSource
from feast.types import Float32, Float64, Int64, String

# Entity
driver = Entity(
    name="driver_id",
    join_keys=["driver_id"],
    description="Unique driver identifier"
)

# Data Source (Parquet file for offline, Redis for online)
driver_stats_source = FileSource(
    path="data/driver_stats.parquet",
    timestamp_field="event_timestamp",
    created_timestamp_column="created_timestamp",
)

# Feature View
driver_stats_fv = FeatureView(
    name="driver_stats",
    entities=[driver],
    ttl=timedelta(days=1),
    schema=[
        Field(name="avg_rating", dtype=Float32),
        Field(name="total_rides", dtype=Int64),
        Field(name="acceptance_rate", dtype=Float32),
        Field(name="cancellation_rate", dtype=Float32),
        Field(name="avg_earnings_per_hour", dtype=Float32),
        Field(name="completion_rate", dtype=Float32),
        Field(name="avg_response_time_seconds", dtype=Float32),
        Field(name="preferred_areas", dtype=String),
        Field(name="vehicle_type", dtype=String),
    ],
    online=True,
    source=driver_stats_source,
    tags={"team": "nidus", "domain": "driver"},
)

# Feature Service (groups features for model serving)
driver_activity_service = FeatureService(
    name="driver_activity",
    features=[driver_stats_fv],
    tags={"model": "matching_engine"},
)