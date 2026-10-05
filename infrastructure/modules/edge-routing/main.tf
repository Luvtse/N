variable "domain" {
  type = string
}

variable "regions" {
  type = list(string)
}

resource "cloudflare_zone" "nidaw" {
  zone = var.domain
}

# Smart DNS routing based on geography
resource "cloudflare_record" "nidaw_api" {
  zone_id = cloudflare_zone.nidaw.id
  name    = "api"
  type    = "CNAME"
  value   = "nidaw-geo-routing.example.com"
  proxied = true
}

# Load balancer for multi-region routing
resource "cloudflare_load_balancer" "nidaw" {
  zone_id          = cloudflare_zone.nidaw.id
  name             = "nidaw-geo-routing.${var.domain}"
  default_pool_ids = [for region in var.regions : cloudflare_load_balancer_pool.regional[region].id]
  fallback_pool_id = cloudflare_load_balancer_pool.regional["us-east-1"].id
  
  rules {
    name      = "eu-routing"
    condition = "(http.request.geo.country in {\"DE\" \"FR\" \"GB\" \"ES\" \"IT\"})"
    fixed_response {
      status_code = 302
      location    = "https://eu.api.${var.domain}"
    }
  }
  
  rules {
    name      = "apac-routing"
    condition = "(http.request.geo.country in {\"JP\" \"KR\" \"SG\" \"AU\" \"IN\"})"
    fixed_response {
      status_code = 302
      location    = "https://apac.api.${var.domain}"
    }
  }
}

# Regional pools
resource "cloudflare_load_balancer_pool" "regional" {
  for_each = toset(var.regions)
  
  name = "nidaw-${each.key}"
  
  origins {
    name    = "k8s-${each.key}"
    address = "k8s-${each.key}.${var.domain}"
    enabled = true
  }
}

# WAF rules
resource "cloudflare_ruleset" "nidaw_waf" {
  zone_id     = cloudflare_zone.nidaw.id
  name        = "NIDAW WAF"
  description = "WAF rules for NIDAW"
  kind        = "zone"
  phase       = "http_request_firewall_managed"
  
  rules {
    action = "execute"
    action_parameters {
      id = "c241234567890"  # Cloudflare managed ruleset
    }
    expression = "true"
    description = "Enable Cloudflare managed rules"
    enabled     = true
  }
}