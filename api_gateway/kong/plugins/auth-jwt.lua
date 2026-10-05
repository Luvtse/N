-- ============================================================================
-- NIDAW Custom JWT Authentication Plugin for Kong
-- Extends built-in JWT with:
--   - Multi-tenant token validation
--   - Role-based access control (RBAC)
--   - Token revocation checking (Redis blacklist)
--   - Automatic token refresh hints
--   - Request context enrichment
-- ============================================================================

local plugin = {
  PRIORITY = 1005,
  VERSION  = "1.0.0",
}

local kong = kong
local ngx = ngx
local string = string
local pairs = pairs
local type = type
local tonumber = tonumber
local os = os

-- ============================================================================
-- CONSTANTS
-- ============================================================================

local PUBLIC_PATHS = {
  ["/health"] = true,
  ["/ready"] = true,
  ["/api/v1/auth/login"] = true,
  ["/api/v1/auth/register"] = true,
  ["/api/v1/nidus/rides/estimate"] = true,
  ["/api/v1/haven/hotels"] = true,
  ["/api/v1/vorax/restaurants"] = true,
}

local ROLE_HIERARCHY = {
  admin      = 100,
  super_user = 90,
  manager    = 70,
  corporate  = 50,
  driver     = 30,
  rider      = 10,
  guest      = 1,
}

local TOKEN_REFRESH_THRESHOLD = 300 -- 5 minutes before expiry

-- ============================================================================
-- HELPERS
-- ============================================================================

local function is_public_path(path)
  if PUBLIC_PATHS[path] then
    return true
  end

  -- Check pattern matches
  for public_path, _ in pairs(PUBLIC_PATHS) do
    if path:sub(1, #public_path) == public_path then
      return true
    end
  end

  return false
end

local function extract_bearer_token(auth_header)
  if not auth_header then
    return nil, "missing_authorization_header"
  end

  local prefix = auth_header:sub(1, 7)
  if prefix:lower() ~= "bearer " then
    return nil, "invalid_token_format"
  end

  local token = auth_header:sub(8)
  if token == "" then
    return nil, "empty_token"
  end

  return token, nil
end

local function decode_jwt_payload(token)
  -- Split token into parts
  local parts = {}
  for part in token:gmatch("[^%.]+") do
    parts[#parts + 1] = part
  end

  if #parts ~= 3 then
    return nil, "invalid_jwt_structure"
  end

  -- Decode payload (base64url)
  local payload_b64 = parts[2]
  -- Convert base64url to base64
  payload_b64 = payload_b64:gsub("-", "+"):gsub("_", "/")
  -- Add padding
  local padding = #payload_b64 % 4
  if padding > 0 then
    payload_b64 = payload_b64 .. string.rep("=", 4 - padding)
  end

  local decoded = ngx.decode_base64(payload_b64)
  if not decoded then
    return nil, "invalid_jwt_encoding"
  end

  -- Parse JSON
  local cjson = require "cjson.safe"
  local payload, err = cjson.decode(decoded)
  if not payload then
    return nil, "invalid_jwt_payload: " .. (err or "unknown")
  end

  return payload, nil
end

local function check_token_revocation(red, token_jti)
  if not token_jti then
    return false -- No JTI, can't check
  end

  local revoked, err = red:get("nidaw:revoked:" .. token_jti)
  if err then
    kong.log.warn("Redis error checking token revocation: ", err)
    return false -- Fail open
  end

  return revoked ~= ngx.null
end

local function get_redis_connection(conf)
  local redis = require "resty.redis"
  local red = redis:new()
  red:set_timeout(conf.redis_timeout or 2000)

  local ok, err = red:connect(conf.redis_host, conf.redis_port)
  if not ok then
    return nil, err
  end

  if conf.redis_password and conf.redis_password ~= "" then
    red:auth(conf.redis_password)
  end

  if conf.redis_database and conf.redis_database > 0 then
    red:select(conf.redis_database)
  end

  return red
end

-- ============================================================================
-- PLUGIN SCHEMA
-- ============================================================================

plugin.schema = {
  name = "nidaw-jwt-auth",
  fields = {
    {
      config = {
        type = "record",
        fields = {
          { redis_host = { type = "string", default = "redis" } },
          { redis_port = { type = "number", default = 6379 } },
          { redis_password = { type = "string", default = "" } },
          { redis_database = { type = "number", default = 1 } },
          { redis_timeout = { type = "number", default = 2000 } },
          { jwt_secret = { type = "string", required = true } },
          { issuer = { type = "string", default = "nidaw" } },
          { audience = { type = "string", default = "nidaw-client" } },
          { enable_revocation = { type = "boolean", default = true } },
          { enable_rbac = { type = "boolean", default = true } },
          { required_roles = { type = "array", elements = { type = "string" }, default = {} } },
          { enable_refresh_hint = { type = "boolean", default = true } },
          { clock_skew = { type = "number", default = 30 } },
          { fault_tolerant = { type = "boolean", default = false } },
        },
      },
    },
  },
}

-- ============================================================================
-- ACCESS PHASE
-- ============================================================================

function plugin:access(conf)
  local request_path = kong.request.get_path()
  local request_method = kong.request.get_method()

  -- Skip authentication for public paths
  if is_public_path(request_path) then
    kong.service.request.set_header("X-Auth-Status", "public")
    return
  end

  -- Skip for OPTIONS (CORS preflight)
  if request_method == "OPTIONS" then
    return
  end

  -- Extract token
  local auth_header = kong.request.get_header("authorization")
  local token, token_err = extract_bearer_token(auth_header)

  if not token then
    return kong.response.exit(401, {
      error = {
        code = "UNAUTHORIZED",
        message = "Authentication required",
        detail = token_err,
      },
    }, {
      ["WWW-Authenticate"] = 'Bearer realm="nidaw"',
    })
  end

  -- Decode JWT payload (without full signature verification - Kong's JWT plugin handles that)
  local payload, decode_err = decode_jwt_payload(token)
  if not payload then
    return kong.response.exit(401, {
      error = {
        code = "INVALID_TOKEN",
        message = "Invalid JWT token",
        detail = decode_err,
      },
    })
  end

  -- Validate issuer
  if conf.issuer and payload.iss and payload.iss ~= conf.issuer then
    return kong.response.exit(401, {
      error = {
        code = "INVALID_ISSUER",
        message = "Token issuer mismatch",
      },
    })
  end

  -- Validate audience
  if conf.audience and payload.aud then
    local aud = payload.aud
    if type(aud) == "string" then
      aud = { aud }
    end
    local aud_match = false
    for _, a in pairs(aud) do
      if a == conf.audience then
        aud_match = true
        break
      end
    end
    if not aud_match then
      return kong.response.exit(401, {
        error = {
          code = "INVALID_AUDIENCE",
          message = "Token audience mismatch",
        },
      })
    end
  end

  -- Validate expiration
  local now = os.time()
  if payload.exp then
    if now > (payload.exp + conf.clock_skew) then
      return kong.response.exit(401, {
        error = {
          code = "TOKEN_EXPIRED",
          message = "Token has expired",
        },
      }, {
        ["X-Token-Expired"] = "true",
      })
    end
  end

  -- Validate not-before
  if payload.nbf then
    if now < (payload.nbf - conf.clock_skew) then
      return kong.response.exit(401, {
        error = {
          code = "TOKEN_NOT_YET_VALID",
          message = "Token is not yet valid",
        },
      })
    end
  end

  -- Check token revocation
  if conf.enable_revocation then
    local red, redis_err = get_redis_connection(conf)
    if red then
      local is_revoked = check_token_revocation(red, payload.jti)
      red:set_keepalive(10000, 100)

      if is_revoked then
        return kong.response.exit(401, {
          error = {
            code = "TOKEN_REVOKED",
            message = "Token has been revoked",
          },
        })
      end
    elseif not conf.fault_tolerant then
      return kong.response.exit(503, {
        error = {
          code = "AUTH_SERVICE_UNAVAILABLE",
          message = "Authentication service unavailable",
        },
      })
    end
  end

  -- Role-Based Access Control
  if conf.enable_rbac and #conf.required_roles > 0 then
    local user_role = payload.role or "rider"
    local user_role_level = ROLE_HIERARCHY[user_role] or 0

    local has_access = false
    for _, required_role in pairs(conf.required_roles) do
      local required_level = ROLE_HIERARCHY[required_role] or 0
      if user_role_level >= required_level then
        has_access = true
        break
      end
    end

    if not has_access then
      return kong.response.exit(403, {
        error = {
          code = "FORBIDDEN",
          message = "Insufficient permissions",
          required_roles = conf.required_roles,
          current_role = user_role,
        },
      })
    end
  end

  -- ========================================================================
  -- ENRICH REQUEST CONTEXT (Pass user info to upstream)
  -- ========================================================================

  kong.service.request.set_header("X-User-ID", tostring(payload.uid or payload.sub or ""))
  kong.service.request.set_header("X-User-Email", tostring(payload.email or ""))
  kong.service.request.set_header("X-User-Role", tostring(payload.role or "rider"))
  kong.service.request.set_header("X-Tenant-ID", tostring(payload.tid or ""))
  kong.service.request.set_header("X-Token-Type", tostring(payload.type or "access"))
  kong.service.request.set_header("X-Auth-Status", "authenticated")

  -- Store in Kong context for other plugins
  kong.ctx.shared.user_id = payload.uid or payload.sub
  kong.ctx.shared.user_email = payload.email
  kong.ctx.shared.user_role = payload.role
  kong.ctx.shared.tenant_id = payload.tid

  -- Token refresh hint
  if conf.enable_refresh_hint and payload.exp then
    local time_until_expiry = payload.exp - now
    if time_until_expiry < TOKEN_REFRESH_THRESHOLD then
      kong.response.set_header("X-Token-Expiring-Soon", "true")
      kong.response.set_header("X-Token-Expires-In", tostring(time_until_expiry))
    end
  end
end

-- ============================================================================
-- LOG PHASE
-- ============================================================================

function plugin:log(conf)
  local user_id = kong.ctx.shared.user_id or "anonymous"
  local user_role = kong.ctx.shared.user_role or "unknown"
  local status = kong.response.get_status()
  local path = kong.request.get_path()
  local method = kong.request.get_method()

  kong.log.info("auth_check",
    " user=", user_id,
    " role=", user_role,
    " method=", method,
    " path=", path,
    " status=", status
  )
end

return plugin