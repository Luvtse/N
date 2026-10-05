# 📊 NIDAW ML Schemas

Event schemas for machine learning pipelines using Apache Avro.

## 📁 Structure

ml/schemas/
├── avro/
│ ├── ride_requested.avsc # Ride request events
│ ├── ride_location.avsc # Location update events
│ └── payment_processed.avsc # Payment events
└── README.md


## 🎯 Purpose

These schemas define the structure of events flowing through the ML pipeline:

1. **ride_requested.avsc**: Captures ride request data for ETA prediction and demand forecasting
2. **ride_location.avsc**: Tracks driver/rider locations for real-time analytics
3. **payment_processed.avsc**: Records payment events for revenue analytics and fraud detection

## 🔧 Usage

### Python (with FastAvro)

```python
from fastavro import parse_schema, writer, reader
import json

# Load schema
with open('ride_requested.avsc', 'r') as f:
    schema = parse_schema(json.load(f))

# Write event
event = {
    "event_id": "550e8400-e29b-41d4-a716-446655440000",
    "event_type": "RIDE_REQUESTED",
    "timestamp": 1720512000000,
    "ride_id": "660e8400-e29b-41d4-a716-446655440001",
    "user_id": "770e8400-e29b-41d4-a716-446655440002",
    # ... other fields
}

with open('events.avro', 'wb') as out:
    writer(out, schema, [event])

# Read events
with open('events.avro', 'rb') as f:
    for record in reader(f):
        print(record)

Java (with Apache Avro)

import org.apache.avro.Schema;
import org.apache.avro.generic.GenericData;
import org.apache.avro.generic.GenericRecord;
import org.apache.avro.file.DataFileWriter;
import org.apache.avro.io.DatumWriter;
import org.apache.avro.generic.GenericDatumWriter;

// Load schema
Schema schema = new Schema.Parser().parse(new File("ride_requested.avsc"));

// Create record
GenericRecord record = new GenericData.Record(schema);
record.put("event_id", "550e8400-e29b-41d4-a716-446655440000");
record.put("event_type", "RIDE_REQUESTED");
// ... other fields

// Write
DatumWriter<GenericRecord> datumWriter = new GenericDatumWriter<>(schema);
DataFileWriter<GenericRecord> dataFileWriter = new DataFileWriter<>(datumWriter);
dataFileWriter.create(schema, new File("events.avro"));
dataFileWriter.append(record);
dataFileWriter.close();

🔄 Schema Evolution
Avro supports schema evolution, allowing you to:
Add new fields with default values
Remove fields (readers will ignore unknown fields)
Change field types (with compatible conversions)
Example: Adding a Field

{
  "name": "surge_multiplier",
  "type": "double",
  "default": 1.0,
  "doc": "Dynamic pricing surge multiplier"
}

📊 Event Flow


┌─────────────┐
│ Ride App    │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ Kafka       │  ← Events published here
└──────┬──────┘
       │
       ├──────────────┬──────────────┐
       ▼              ▼              ▼
┌─────────────┐ ┌─────────────┐ ┌─────────────┐
│ Feature     │ │ Real-time   │ │ Data        │
│ Pipeline    │ │ Analytics   │ │ Warehouse   │
└──────┬──────┘ └─────────────┘ └─────────────┘
       │
       ▼
┌─────────────┐
│ Feature     │
│ Store       │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ ML Models   │
└─────────────┘

🛡️ Validation
All events are validated against schemas before being published to Kafka:

from confluent_kafka.avro import AvroProducer

producer = AvroProducer({
    'bootstrap.servers': 'kafka:9092',
    'schema.registry.url': 'http://schema-registry:8081'
})

# Schema is automatically validated
producer.produce(
    topic='nidus.rides',
    value=event,
    value_schema=schema
)

