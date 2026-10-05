# ============================================================================
# NIDAW Custom Kong Gateway Image
# Includes custom plugins and optimized configuration
# ============================================================================

# --------------------------------------------------------------------------
# BUILD STAGE
# --------------------------------------------------------------------------
FROM kong:3.5.0 AS builder

USER root

# Install build dependencies
RUN apk add --no-cache \
    git \
    gcc \
    musl-dev \
    make \
    curl

# Create plugin directories
RUN mkdir -p /usr/local/share/lua/5.1/kong/plugins/nidaw-rate-limiting \
    && mkdir -p /usr/local/share/lua/5.1/kong/plugins/nidaw-jwt-auth

# Copy custom plugins
COPY plugins/rate-limiting.lua /usr/local/share/lua/5.1/kong/plugins/nidaw-rate-limiting/handler.lua
COPY plugins/auth-jwt.lua /usr/local/share/lua/5.1/kong/plugins/nidaw-jwt-auth/handler.lua

# Install additional Lua dependencies
RUN luarocks install lua-resty-redis 0.30 \
    && luarocks install lua-resty-jwt 0.2.3 \
    && luarocks install lua-cjson 2.1.0.14

# --------------------------------------------------------------------------
# RUNTIME STAGE
# --------------------------------------------------------------------------
FROM kong:3.5.0

LABEL maintainer="NIDAW Engineering <engineering@nidaw.com>"
LABEL description="NIDAW API Gateway with custom plugins"
LABEL version="1.0.0"

USER root

# Copy custom plugins from builder
COPY --from=builder /usr/local/share/lua/5.1/kong/plugins/ /usr/local/share/lua/5.1/kong/plugins/
COPY --from=builder /usr/local/lib/lua/5.1/ /usr/local/lib/lua/5.1/

# Install runtime dependencies
RUN apk add --no-cache \
    curl \
    tini \
    openssl \
    && addgroup -S nidaw \
    && adduser -S nidaw -G nidaw

# Copy configuration
COPY kong.yml /etc/kong/kong.yml

# Create required directories
RUN mkdir -p /var/log/kong \
    && mkdir -p /tmp/kong \
    && chown -R nidaw:nidaw /var/log/kong \
    && chown -R nidaw:nidaw /tmp/kong \
    && chown -R nidaw:nidaw /usr/local/kong

# Set environment variables
ENV KONG_DATABASE=off \
    KONG_DECLARATIVE_CONFIG=/etc/kong/kong.yml \
    KONG_PROXY_ACCESS_LOG=/dev/stdout \
    KONG_ADMIN_ACCESS_LOG=/dev/stdout \
    KONG_PROXY_ERROR_LOG=/dev/stderr \
    KONG_ADMIN_ERROR_LOG=/dev/stderr \
    KONG_ADMIN_LISTEN=0.0.0.0:8001 \
    KONG_PROXY_LISTEN=0.0.0.0:8000,0.0.0.0:8443 ssl \
    KONG_STATUS_LISTEN=0.0.0.0:8100 \
    KONG_PLUGINS=bundled,nidaw-rate-limiting,nidaw-jwt-auth \
    KONG_NGINX_WORKER_PROCESSES=auto \
    KONG_NGINX_WORKER_CONNECTIONS=4096 \
    KONG_MEM_CACHE_SIZE=512m \
    KONG_PROXY_BUFFER_SIZE=128k \
    KONG_PROXY_BUFFERS="64 128k" \
    KONG_LOG_LEVEL=info \
    KONG_TRUSTED_IPS=0.0.0.0/0,::/0 \
    KONG_REAL_IP_HEADER=X-Forwarded-For \
    KONG_REAL_IP_RECURSIVE=on

# Expose ports
# 8000 - HTTP proxy
# 8443 - HTTPS proxy
# 8001 - Admin API
# 8100 - Status API
# 8444 - Admin API SSL
EXPOSE 8000 8443 8001 8100 8444

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD curl -f http://localhost:8100/status || exit 1

# Switch to non-root user
USER nidaw

# Use tini as init system
ENTRYPOINT ["/sbin/tini", "--"]

# Start Kong
CMD ["kong", "start", "--vv"]