from datetime import timedelta
from feast import Entity, FeatureService, FeatureView, Field, FileSource
from feast.types import Float32, Float64, Int64

# H3 hexagonal grid entity for geographic demand
h3_cell = Entity(
    name="h3_cell",
    join_keys=["h3_cell"],
    description="H3 hexagonal grid cell ID"
)

demand_source = FileSource(
    path="data/demand_features.parquet",
    timestamp_field="event_timestamp",
)

demand_features_fv = FeatureView(
    name="demand_features",
    entities=[h3_cell],
    ttl=timedelta(hours=1),
    schema=[
        Field(name="hour_of_day", dtype=Int64),
        Field(name="day_of_week", dtype=Int64),
        Field(name="requests_last_hour", dtype=Int64),
        Field(name="requests_last_24h", dtype=Int64),
        Field(name="avg_wait_time_minutes", dtype=Float32),
        Field(name="available_drivers", dtype=Int64),
        Field(name="demand_supply_ratio", dtype=Float32),
        Field(name="historical_avg_requests", dtype=Float32),
        Field(name="is_holiday", dtype=Int64),
        Field(name="event_nearby", dtype=Int64),
    ],
    online=True,
    source=demand_source,
    tags={"team": "nidus", "domain": "demand"},
)

demand_service = FeatureService(
    name="demand_forecasting",
    features=[demand_features_fv],
)