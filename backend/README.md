Built with ❤️ by the NIDAW Team


### `backend/README.md`

```markdown
# NIDAW Backend

Go-based modular monolith backend for the NIDAW platform.

## 🏗️ Architecture

- **Pattern**: Modular Monolith with CQRS + Event Sourcing
- **Framework**: Chi router
- **Database**: PostgreSQL with pgx driver
- **Cache**: Redis
- **Messaging**: Apache Kafka
- **Auth**: JWT with bcrypt password hashing

## 📁 Project Structure

backend/
├── cmd/
│ └── server/ # Application entry point
├── internal/
│ ├── modules/ # Feature modules
│ │ ├── auth/ # Authentication
│ │ ├── nidus/ # Rides
│ │ ├── haven/ # Hotels
│ │ ├── vorax/ # Food
│ │ ├── logix/ # Freight
│ │ └── corpus/ # Corporate
│ ├── shared/ # Shared services
│ │ ├── auth/ # JWT & middleware
│ │ ├── database/ # DB connection
│ │ ├── eventbus/ # Kafka
│ │ └── middleware/ # HTTP middleware
│ └── pkg/ # Utilities
├── migrations/ # Database migrations
└── api/ # OpenAPI specs



## 🚀 Getting Started

### Prerequisites

- Go 1.21+
- PostgreSQL 15+
- Redis 7+
- Apache Kafka 3.5+

### Setup

1. Copy environment file:
```bash
cp .env.example .env

2. Install dependencies:

go mod download

3. Run migrations:

go run cmd/migrate/main.go --up

4. Start server:

go run cmd/server/main.go

🧪 Testing

# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run specific test
go test ./internal/modules/nidus/...

📡 API Endpoints
See OpenAPI Specification for complete API documentation.
Authentication
POST /api/v1/auth/register - Register new user
POST /api/v1/auth/login - Login
POST /api/v1/auth/refresh - Refresh token
GET /api/v1/auth/me - Get current user
Rides
POST /api/v1/nidus/rides - Request ride
GET /api/v1/nidus/rides - List rides
GET /api/v1/nidus/rides/:id - Get ride details
POST /api/v1/nidus/rides/estimate - Get fare estimate

🐳 Docker

# Build image
docker build -t nidaw-backend .

# Run container
docker run -p 8080:8080 --env-file .env nidaw-backend

📊 Monitoring
Metrics: Prometheus at /metrics
Health: /health and /ready endpoints
Tracing: OpenTelemetry with Jaeger
🔒 Security
JWT authentication
Rate limiting
CORS protection
Input validation
SQL injection prevention
XSS protection

NIDAW Backend - Built with Go