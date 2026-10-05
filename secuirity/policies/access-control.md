# Access Control Policy

**Document ID:** NIDAW-SEC-POL-001  
**Version:** 1.0  
**Effective Date:** July 2026  
**Owner:** Security Team  
**Review Cadence:** Quarterly

## 🎯 Purpose

This policy defines the access control framework for the NIDAW platform, ensuring that users, services, and systems have only the minimum necessary access to perform their functions (Principle of Least Privilege).

## 📋 Scope

This policy applies to:
- All NIDAW employees and contractors
- All NIDAW users (riders, drivers, corporate clients, partners)
- All NIDAW services and APIs
- All NIDAW infrastructure (cloud, on-premise, hybrid)
- All third-party integrations

## 🔐 Authentication Requirements

### User Authentication

| Requirement | Standard | MFA Required |
|-------------|----------|--------------|
| **Password Length** | Minimum 12 characters | N/A |
| **Password Complexity** | Upper, lower, number, special | N/A |
| **Password History** | Last 12 passwords | N/A |
| **Password Expiry** | 90 days | N/A |
| **Account Lockout** | 5 failed attempts | N/A |
| **Lockout Duration** | 30 minutes | N/A |
| **Session Timeout** | 30 minutes (idle) | N/A |
| **MFA** | Required for all accounts | TOTP, WebAuthn, SMS |

### Service Authentication

| Service Type | Authentication Method | Token Lifetime |
|--------------|----------------------|----------------|
| **User-facing APIs** | JWT (RS256) | 15 minutes |
| **Service-to-service** | mTLS + JWT | 1 hour |
| **Third-party integrations** | OAuth 2.0 + API keys | 1 hour |
| **Admin APIs** | mTLS + MFA | 15 minutes |
| **Webhooks** | HMAC signatures | Per-request |

### Infrastructure Authentication

| System | Authentication | MFA |
|--------|---------------|-----|
| **AWS Console** | IAM + MFA | Required |
| **Kubernetes** | OIDC + RBAC | Required |
| **GitHub** | SSO + MFA | Required |
| **Databases** | IAM authentication | Required |
| **CI/CD** | OIDC federation | Required |

## 👥 Role-Based Access Control (RBAC)

### User Roles

#### Rider Role
```yaml
permissions:
  - ride.create
  - ride.read_own
  - ride.cancel_own
  - ride.rate
  - hotel.search
  - hotel.book
  - hotel.read_own
  - hotel.cancel_own
  - food.order
  - food.read_own
  - food.cancel_own
  - payment.create
  - payment.read_own
  - profile.read_own
  - profile.update_own

Driver Role

permissions:
  - ride.accept
  - ride.complete
  - ride.read_assigned
  - ride.rate_rider
  - location.update_own
  - earnings.read_own
  - profile.read_own
  - profile.update_own
  - vehicle.read_own
  - vehicle.update_own
  - document.upload_own

Corporate Admin Role

permissions:
  - corporate.read
  - corporate.update
  - employee.create
  - employee.read
  - employee.update
  - employee.delete
  - policy.create
  - policy.read
  - policy.update
  - expense.read_all
  - expense.approve
  - report.read
  - billing.read

Partner Role (Hotels, Restaurants)

permissions:
  - partner.read_own
  - partner.update_own
  - listing.create
  - listing.read_own
  - listing.update_own
  - listing.delete_own
  - order.read_own
  - order.update_own
  - booking.read_own
  - booking.update_own
  - earnings.read_own
  - review.read_own
  - review.respond_own

Admin Role

permissions:
  - admin.*  # Full access
  - audit.read
  - user.manage
  - system.configure
  - security.manage
  - compliance.manage

Service Roles
Backend Service

permissions:
  - service.read_own_data
  - service.write_own_data
  - event.publish
  - event.subscribe_own
  - metrics.publish
  - logs.write

Analytics Service

permissions:
  - analytics.read_aggregated
  - analytics.write_reports
  - data.read_anonymized
  - metrics.read

ML Service

permissions:
  - ml.read_features
  - ml.write_predictions
  - ml.read_models
  - ml.write_models
  - data.read_anonymized

🏗️ Kubernetes RBAC
Namespace Isolation

# Production namespace
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  namespace: nidaw-production
  name: backend-role
rules:
  - apiGroups: [""]
    resources: ["pods", "services", "configmaps", "secrets"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["apps"]
    resources: ["deployments", "replicasets"]
    verbs: ["get", "list", "watch"]

Service Account Permissions

# Backend service account
apiVersion: v1
kind: ServiceAccount
metadata:
  name: nidaw-backend
  namespace: nidaw-production
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/nidaw-backend

# Limited permissions
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: nidaw-backend-binding
  namespace: nidaw-production
subjects:
  - kind: ServiceAccount
    name: nidaw-backend
    namespace: nidaw-production
roleRef:
  kind: Role
  name: backend-role
  apiGroup: rbac.authorization.k8s.io

🔑 API Access Control
API Key Management

Key Type	Lifetime	Rotation	Storage
Production	90 days		Automatic	Secrets Manager
Staging		180 days	Manual		Secrets Manager
Development	365 days	Manual		Local .env
Third-party	90 days		Manual		Secrets Manager

Rate Limiting

Tier	Requests/Minute	Requests/Hour	Burst
Free		30	500		10
Premium		200	5,000		50
Enterprise	1,000	30,000		200
Internal	10,000	100,000		1,000


Endpoint Authorization

// Example: Middleware for endpoint authorization
func RequirePermission(permission string) gin.HandlerFunc {
    return func(c *gin.Context) {
        user := GetUserFromContext(c)
        
        if !user.HasPermission(permission) {
            c.AbortWithStatusJSON(403, gin.H{
                "error": "Insufficient permissions",
                "required": permission,
            })
            return
        }
        
        c.Next()
    }
}

// Usage
router.POST("/api/v1/nidus/rides", 
    RequireAuth(),
    RequirePermission("ride.create"),
    CreateRideHandler,
)

🔒 Data Access Control
Row-Level Security

-- PostgreSQL RLS for rides table
ALTER TABLE rides ENABLE ROW LEVEL SECURITY;

-- Users can only see their own rides
CREATE POLICY rides_user_policy ON rides
    FOR SELECT
    USING (user_id = current_setting('app.current_user_id')::uuid);

-- Drivers can see assigned rides
CREATE POLICY rides_driver_policy ON rides
    FOR SELECT
    USING (driver_id = current_setting('app.current_driver_id')::uuid);

-- Admins can see all rides
CREATE POLICY rides_admin_policy ON rides
    FOR ALL
    USING (current_setting('app.is_admin')::boolean = true);

Column-Level Security

// Example: Field-level encryption for PII
type User struct {
    ID        uuid.UUID `json:"id"`
    Email     string    `json:"email"`               // Encrypted
    Phone     string    `json:"phone"`               // Encrypted
    FullName  string    `json:"full_name"`           // Encrypted
    Role      string    `json:"role"`                // Not encrypted
    CreatedAt time.Time `json:"created_at"`          // Not encrypted
}

// Encrypt PII fields before storage
func (u *User) Encrypt(key []byte) error {
    encrypted, err := encryption.Encrypt([]byte(u.Email), key)
    if err != nil {
        return err
    }
    u.Email = base64.StdEncoding.EncodeToString(encrypted)
    // Repeat for other PII fields
    return nil
}

🛡️ Privileged Access Management
Break-Glass Accounts

Account			Purpose			Access Level	Monitoring
break-glass-admin	Emergency access	Full admin	Real-time alert
break-glass-db		Database emergency	DB admin	Real-time alert
break-glass-infra      Infrastructure emergencyInfra admin	Real-time alert

Just-In-Time Access

# AWS IAM Identity Center permission boundary
Version: 2012-10-17
Statement:
  - Effect: Allow
    Action:
      - "s3:GetObject"
      - "s3:PutObject"
    Resource: "arn:aws:s3:::nidaw-data/*"
    Condition:
      Bool:
        "aws:MultiFactorAuthPresent": "true"
      DateLessThan:
        "aws:CurrentTime": "${session_end_time}"

Access Request Workflow
Request: User submits access request via portal
Approval: Manager + Security Team approval
Provisioning: Temporary access granted (max 24 hours)
Monitoring: All actions logged and monitored
Revocation: Access automatically revoked after expiry
Review: Post-access review by Security Team
📊 Access Review
Quarterly Access Reviews

Review Type	Frequency	Owner	Scope
User Access	Quarterly	IT Security	All user accounts
Service AccountsQuarterly	Platform Team	All service accounts
Admin Access	Monthly		Security Team	All admin accounts
Third-party AccessQuarterly	Vendor Management	All vendor accounts

Access Certification

# Access certification workflow
certification:
  frequency: quarterly
  scope:
    - all_users
    - all_service_accounts
    - all_admin_accounts
  approvers:
    - direct_manager
    - data_owner
    - security_team
  evidence:
    - access_logs
    - last_login
    - permission_usage
  remediation:
    - revoke_unused_access
    - document_business_justification
    - escalate_to_security

🚨 Access Control Monitoring
Alerts

Alert		  	 Condition	    Severity	Response
Failed Login	>     5 attempts in 5 min	High	Account lockout
Privilege Escalation	Role change detected	Critical	Immediate review
Unusual Access	        Access from new location Medium	MFA challenge
Admin Activity	        Any admin action	Low	Log & monitor
Service Account	  Outside business hours	High	Investigate

udit Logging
All access control events are logged:
Authentication attempts (success/failure)
Authorization decisions (allow/deny)
Permission changes
Role assignments
Access reviews
Privileged actions
📚 Compliance Mapping

Control		SOC 2	PCI DSS	GDPR	ISO 27001
Authentication	CC6.1	8.2	Art. 32	A.9.2
Authorization	CC6.2	7.2	Art. 25	A.9.1
Access Review	CC6.3	8.6	Art. 30	A.9.2.5
Privileged AccessCC6.4	7.1	Art. 32	A.9.2.3
Audit Logging	CC7.2	10.2	Art. 30	A.12.4

📞 Contact
Policy Owner: Security Team
Email: security@nidaw.com
Slack: #security-policy
Emergency: security-incident@nidaw.com
Last Updated: July 2026
Version: 1.0
Next Review: October 2026
Classification: Internal