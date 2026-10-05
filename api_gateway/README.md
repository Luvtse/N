# 🚪 NIDAW API Gateway

Kong-based API Gateway for the NIDAW platform. Provides authentication, rate limiting, caching, logging, and traffic management for all backend services.

## 🏗️ Architecture

                ┌─────────────────────────────────┐
                │        Cloudflare CDN/WAF       │
                └──────────────┬──────────────────┘
                               │
                ┌──────────────▼──────────────────┐
                │      Kong API Gateway           │
                │  ┌───────────────────────────┐  │
                │  │  JWT Auth (nidaw-jwt)     │  │
                │  │  Rate Limiting (tiered)   │  │
                │  │  CORS                     │  │
                │  │  Request/Response Transform│  │
                │  │  Bot Detection            │  │
                │  │  Prometheus Metrics       │  │
                │  │  OpenTelemetry Tracing    │  │
                │  │  HTTP Logging (Loki)      │  │
                │  └───────────────────────────┘  │
                └──────┬───────┬───────┬──────────┘
                       │       │       │
          ┌────────────▼──┐ ┌──▼───────▼──┐ ┌────▼────────┐
          │ Nidus (Rides) │ │Haven(Hotels)│ │Vorax (Food) │
          │ :8080         │ │:8080        │ │:8080        │
          └───────────────┘ └─────────────┘ └─────────────┘


## 📁 Project Structure

api-gateway/
├── kong/
│ ├── kong.yml # Main declarative configuration
│ ├── Dockerfile # Custom Kong image
│ └── plugins/
│ ├── rate-limiting.lua # Tiered rate limiting plugin
│ └── auth-jwt.lua # JWT auth + RBAC plugin
├── routes/
│ ├── nidus.yaml # Ride service routes
│ ├── haven.yaml # Hotel service routes
│ └── vorax.yaml # Food service routes
└── README.md


## 🚀 Quick Start

### Local Development

```bash
# Start Kong with Docker Compose
docker-compose -f docker/compose/base.yml up -d kong

# Verify Kong is running
curl http://localhost:8001/status

# Test a public endpoint
curl http://localhost:8000/api/v1/haven/hotels?city=New+York

# Test an authenticated endpoint
curl -H "Authorization: Bearer <your-jwt-token>" \
     http://localhost:8000/api/v1/nidus/rides

Production Deployment

# Build custom Kong image
cd api-gateway/kong
docker build -t nidaw/kong-gateway:1.0.0 .

# Deploy via Helm
helm upgrade --install nidaw-gateway ./helm/nidaw-gateway \
  --namespace production \
  --set image.tag=1.0.0

🔐 Authentication
JWT Flow
1. Client calls POST /api/v1/auth/login with credentials
2. Backend returns JWT access token (15 min) + refresh token (7 days)
3. Client includes token in Authorization: Bearer <token> header
4. Kong validates token signature, expiration, issuer, audience
5. Kong checks token revocation list in Redis
6. Kong performs RBAC check based on user role
7. Kong injects user context headers to upstream service

Token Headers Injected to Upstream

  Header	       Description
X-User-ID	Authenticated user UUID
X-User-Email	User email address
X-User-Role	User role (rider, driver, admin)
X-Tenant-ID	Tenant/corporate ID
X-Token-Type	Token type (access, refresh)
X-Auth-Status	Authentication status


Token Refresh Hints
When a token is within 5 minutes of expiration, Kong adds:
   X-Token-Expiring-Soon: true
   X-Token-Expires-In: <seconds>

🚦 Rate Limiting
Tier-Based Limits

Tier	Per Minute	Per Hour       Per Day	 Burst
Free	      30	500	        5,000	 10
Premium	     200	5,000	        50,000 	 50
Enterprise  1,000	30,000	        500,000	 200
Driver	     300	10,000	        100,000	 100
Internal   10,000	100,000	      1,000,000	 1,000

Endpoint-Specific Limits

Endpoint	  Per Minute	Per Hour
POST /rides     	10	100
POST /auth/login	5	20
POST /auth/register	3	10
GET /hotels	       30	500
POST /orders	       15	150

Response Headers

Header	                            Description
X-RateLimit-Limit-Minute	Maximum requests per minute
X-RateLimit-Remaining-Minute	Remaining requests this minute
X-RateLimit-Limit-Hour	        Maximum requests per hour
X-RateLimit-Remaining-Hour	Remaining requests this hour
X-RateLimit-Tier	        Consumer tier
X-RateLimit-Reset	        Unix timestamp when limit resets
Retry-After	                Seconds to wait (on 429)

🛡️ Security Features
CORS: Configured for allowed origins only
Security Headers: HSTS, X-Frame-Options, CSP, etc.
Bot Detection: Blocks known malicious bots
Request Size Limiting: 10MB max payload
IP Restriction: Configurable blocklist
Token Revocation: Redis-backed blacklist
RBAC: Role-based access control per route

📊 Monitoring
Prometheus Metrics
Available at http://localhost:8001/metrics:
Request count by service, route, status code
Latency histograms (proxy, upstream, total)
Bandwidth metrics
Consumer-level metrics
Upstream health status
Distributed Tracing

OpenTelemetry traces sent to Jaeger:
Full request lifecycle
Upstream service timing
Plugin execution timing

Logging
HTTP logs sent to Loki:
Request/response details
Consumer information
Latency breakdowns
Error details

🔧 Configuration
Environment Variables

Variable	          Description	             Default
JWT_SECRET	       JWT signing secret	    Required
KONG_DATABASE	       Database mode	            off
KONG_LOG_LEVEL	       Log level	            info
KONG_PROXY_LISTEN      Proxy listen address	    0.0.0.0:8000
KONG_ADMIN_LISTEN      Admin listen address	    0.0.0.0:8001

Adding a New Route
1. Create route definition in routes/<service>.yaml
2. Add to kong.yml services section
3. Configure plugins (auth, rate limiting, caching)
4. Test locally with docker-compose
5. Deploy via CI/CD pipeline

🧪 Testing

# Run Kong configuration validation
docker run --rm -v $(pwd)/kong/kong.yml:/kong.yml kong:3.5 \
  kong config parse /kong.yml

# Test rate limiting
for i in {1..20}; do
  curl -s -o /dev/null -w "%{http_code}\n" \
    http://localhost:8000/api/v1/auth/login
done

# Test JWT authentication
TOKEN=$(curl -s -X POST http://localhost:8000/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"test@nidaw.com","password":"TestPass123!"}' \
  | jq -r '.access_token')

curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8000/api/v1/nidus/rides

📞 Support
Documentation: docs.nidaw.com/api-gateway
Issues: github.com/nidaw/api-gateway/issues
Slack: #platform-gateway

NIDAW API Gateway - Powered by Kong 🚀


---

## ✅ Summary

| File | Purpose | Status |
|------|---------|--------|
| `kong/kong.yml` | Main declarative config (services, routes, plugins, consumers, upstreams) | ✅ |
| `kong/plugins/rate-limiting.lua` | Tiered rate limiting with token bucket + sliding window | ✅ |
| `kong/plugins/auth-jwt.lua` | JWT auth with RBAC, revocation, context enrichment | ✅ |
| `kong/Dockerfile` | Custom Kong image with plugins | ✅ |
| `routes/nidus.yaml` | 12 ride/driver/location routes | ✅ |
| `routes/haven.yaml` | 9 hotel/booking/review routes | ✅ |
| `routes/vorax.yaml` | 12 restaurant/order/delivery routes | ✅ |
| `README.md` | Complete documentation | ✅ |

**Key Features:**
- ✅ DB-less declarative mode (no database needed)
- ✅ 5-tier rate limiting (free → internal)
- ✅ Token bucket + sliding window algorithms
- ✅ JWT with RBAC, revocation, refresh hints
- ✅ Per-endpoint rate limits
- ✅ Response caching for public endpoints
- ✅ Prometheus metrics + OpenTelemetry tracing
- ✅ Security headers + bot detection
- ✅ 33 total routes across 3 services
- ✅ Health checks + upstream load balancing