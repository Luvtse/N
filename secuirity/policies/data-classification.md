
---

## 📄 File #3: `security/policies/data-classification.md`

```markdown
# Data Classification Policy

**Document ID:** NIDAW-SEC-POL-002  
**Version:** 1.0  
**Effective Date:** July 2026  
**Owner:** Privacy & Security Team  
**Review Cadence:** Quarterly

## 🎯 Purpose

This policy establishes a framework for classifying NIDAW data based on sensitivity and criticality, ensuring appropriate protection measures are applied to each classification level.

## 📋 Scope

This policy applies to:
- All data created, processed, stored, or transmitted by NIDAW
- All NIDAW employees, contractors, and partners
- All NIDAW systems, applications, and infrastructure
- All third-party services handling NIDAW data

## 🏷️ Data Classification Levels

### Level 1: Public

**Definition:** Information that can be freely shared with the public without risk to NIDAW or its users.

**Examples:**
- Marketing materials
- Public website content
- Press releases
- Job postings
- Published blog posts
- Public API documentation
- Open-source code (where applicable)

**Handling Requirements:**
- ✅ No encryption required
- ✅ Can be shared publicly
- ⚠️ Must not contain other classification levels
- ⚠️ Review before publication

**Storage:**
- Public CDN
- Public S3 buckets
- Public GitHub repositories

### Level 2: Internal

**Definition:** Information intended for NIDAW employees and authorized contractors. Disclosure could cause minor operational impact.

**Examples:**
- Internal policies and procedures
- Employee directories
- Internal communications
- Meeting notes
- Training materials
- Internal dashboards
- Non-sensitive operational metrics
- Development documentation

**Handling Requirements:**
- ✅ Accessible to all employees
- ⚠️ Not for external sharing
- ⚠️ Watermark with "INTERNAL"
- ⚠️ Share via secure channels only

**Storage:**
- Internal SharePoint
- Internal Confluence
- Internal Slack channels
- Encrypted internal S3 buckets

### Level 3: Confidential

**Definition:** Sensitive information that requires protection. Unauthorized disclosure could cause significant harm to NIDAW, its users, or partners.

**Examples:**
- **User PII:**
  - Full names
  - Email addresses
  - Phone numbers
  - Home addresses
  - Date of birth
- **Business Data:**
  - Financial records
  - Revenue data
  - Business strategies
  - M&A discussions
  - Product roadmaps
- **Technical Data:**
  - Source code (proprietary)
  - Database schemas
  - API keys (non-production)
  - System architecture
  - Security configurations
- **Partner Data:**
  - Contract terms
  - Pricing agreements
  - Integration details

**Handling Requirements:**
- ✅ Access on need-to-know basis
- ✅ Encryption at rest (AES-256)
- ✅ Encryption in transit (TLS 1.3)
- ✅ Access logging required
- ✅ DLP scanning enabled
- ⚠️ Watermark with "CONFIDENTIAL"
- ❌ No personal devices
- ❌ No public cloud storage
- ❌ No email attachments (use secure links)

**Storage:**
- Encrypted databases
- Encrypted S3 buckets (SSE-KMS)
- Encrypted file shares
- Secrets Manager

### Level 4: Restricted

**Definition:** Highly sensitive information requiring maximum protection. Unauthorized disclosure could cause severe harm, regulatory penalties, or legal liability.

**Examples:**
- **Payment Data (PCI):**
  - Credit card numbers
  - CVV codes
  - Bank account details
  - Payment tokens
- **Authentication Data:**
  - Password hashes
  - Private keys
  - MFA secrets
  - Session tokens
- **Security Data:**
  - Encryption keys
  - Security logs
  - Vulnerability reports
  - Incident reports
  - Penetration test results
- **Regulated Data:**
  - Health information (HIPAA)
  - Government IDs
  - Social Security Numbers
  - Tax identification numbers
- **Critical Business Data:**
  - Master encryption keys
  - Root certificates
  - Admin credentials
  - Disaster recovery plans

**Handling Requirements:**
- ✅ Strict need-to-know access
- ✅ Strong encryption (AES-256-GCM)
- ✅ Hardware Security Modules (HSM)
- ✅ Multi-party authorization
- ✅ Real-time monitoring
- ✅ Immutable audit logs
- ✅ Data loss prevention (DLP)
- ⚠️ Watermark with "RESTRICTED"
- ❌ No email transmission
- ❌ No printing
- ❌ No screenshots
- ❌ No local storage
- ❌ No personal devices

**Storage:**
- HSM-protected databases
- Secrets Manager with rotation
- Encrypted vaults
- Air-gapped systems (where applicable)

## 📊 Classification Matrix

| Data Type | Classification | Retention | Encryption | Access Control |
|-----------|----------------|-----------|------------|----------------|
| User email | Confidential | 7 years | AES-256 | RBAC |
| User phone | Confidential | 7 years | AES-256 | RBAC |
| User name | Confidential | 7 years | AES-256 | RBAC |
| User address | Confidential | 7 years | AES-256 | RBAC |
| Ride history | Confidential | 7 years | AES-256 | RBAC |
| GPS locations | Confidential | 90 days | AES-256 | RBAC |
| Payment tokens | Restricted | 1 year | HSM | Strict |
| Credit card (PAN) | Restricted | Never | HSM | PCI-only |
| CVV | Restricted | Never | HSM | PCI-only |
| Passwords | Restricted | N/A | bcrypt | Auth only |
| API keys (prod) | Restricted | 90 days | KMS | Admin only |
| Source code | Confidential | Permanent | AES-256 | Dev team |
| Financial reports | Confidential | 7 years | AES-256 | Finance |
| Marketing materials | Public | Permanent | None | Public |
| Internal policies | Internal | Permanent | AES-256 | Employees |

## 🔄 Data Lifecycle Management

### Creation

1. **Classification at Creation:**
   - All new data must be classified at creation
   - Default classification: **Confidential** (when in doubt)
   - Use data classification labels in metadata

2. **Automated Classification:**
   - DLP tools scan for PII, PCI, PHI
   - ML models classify based on patterns
   - Manual review for ambiguous cases

### Storage

1. **Storage Requirements by Level:**

| Level | Encryption | Access Control | Backup | Monitoring |
|-------|-----------|----------------|--------|------------|
| Public | Optional | None | Standard | Basic |
| Internal | At rest | RBAC | Standard | Basic |
| Confidential | At rest + transit | RBAC + MFA | Encrypted | Enhanced |
| Restricted | HSM + at rest + transit | Strict + MFA | Encrypted + geo-redundant | Real-time |

2. **Storage Locations:**

| Level | Approved Locations |
|-------|-------------------|
| Public | CDN, public S3, public GitHub |
| Internal | Internal SharePoint, Confluence, Slack |
| Confidential | Encrypted S3, encrypted databases, encrypted file shares |
| Restricted | HSM, Secrets Manager, encrypted vaults, air-gapped systems |

### Usage

1. **Access Controls:**

| Level | Authentication | Authorization | Session |
|-------|---------------|---------------|---------|
| Public | None | None | N/A |
| Internal | SSO | RBAC | 8 hours |
| Confidential | SSO + MFA | RBAC + ABAC | 4 hours |
| Restricted | SSO + MFA + Hardware token | Strict RBAC | 15 minutes |

2. **Sharing Restrictions:**

| Level | Internal Sharing | External Sharing | Printing | Email |
|-------|-----------------|------------------|----------|-------|
| Public | ✅ Unrestricted | ✅ Allowed | ✅ Allowed | ✅ Allowed |
| Internal | ✅ All employees | ⚠️ With approval | ⚠️ Watermarked | ⚠️ Internal only |
| Confidential | ⚠️ Need-to-know | ❌ Generally prohibited | ❌ Prohibited | ❌ Secure links only |
| Restricted | ❌ Strict need-to-know | ❌ Prohibited | ❌ Prohibited | ❌ Prohibited |

### Transmission

1. **Encryption Requirements:**

| Level | In Transit | At Rest | Key Management |
|-------|-----------|---------|----------------|
| Public | Optional | Optional | N/A |
| Internal | TLS 1.2+ | AES-256 | KMS |
| Confidential | TLS 1.3 | AES-256-GCM | KMS + rotation |
| Restricted | TLS 1.3 + mTLS | AES-256-GCM + HSM | HSM + rotation |

2. **Transfer Methods:**

| Level | Approved Methods |
|-------|-----------------|
| Public | Any method |
| Internal | Encrypted email, secure file transfer, internal tools |
| Confidential | Encrypted file transfer, secure APIs, encrypted email |
| Restricted | Secure APIs only, HSM-to-HSM transfer, physical media (encrypted) |

### Archival

1. **Archival Requirements:**

| Level | Archive Format | Archive Location | Retention |
|-------|---------------|------------------|-----------|
| Public | Standard | S3 Standard | As needed |
| Internal | Encrypted | S3 Standard-IA | 3 years |
| Confidential | Encrypted + compressed | S3 Glacier | 7 years |
| Restricted | Encrypted + HSM | S3 Glacier Deep Archive | 10 years |

2. **Archive Access:**

| Level | Access Method | Approval |
|-------|--------------|----------|
| Public | Direct | None |
| Internal | Direct | None |
| Confidential | Request-based | Manager approval |
| Restricted | Multi-party approval | CISO + Legal + Data Owner |

### Destruction

1. **Destruction Methods:**

| Level | Digital Destruction | Physical Destruction |
|-------|--------------------|---------------------|
| Public | Delete | Shred |
| Internal | Secure delete | Shred |
| Confidential | Crypto-shredding | Degauss + shred |
| Restricted | HSM key deletion + crypto-shredding | Incinerate |

2. **Destruction Verification:**

| Level | Verification Method | Documentation |
|-------|--------------------|---------------|
| Public | None | None |
| Internal | Audit log | Certificate |
| Confidential | Cryptographic proof | Certificate + audit |
| Restricted | Third-party verification | Certificate + audit + legal |

## 🛡️ Data Protection Controls

### Encryption

1. **Encryption Standards:**

| Purpose | Algorithm | Key Size | Mode |
|---------|-----------|----------|------|
| Data at rest | AES | 256-bit | GCM |
| Data in transit | TLS | 256-bit | 1.3 |
| Password hashing | bcrypt | N/A | Cost 12 |
| File hashing | SHA-256 | 256-bit | N/A |
| Digital signatures | RSA / ECDSA | 2048+ / 256+ | N/A |

2. **Key Management:**

| Key Type | Storage | Rotation | Backup |
|----------|---------|----------|--------|
| Master keys | HSM | Annual | HSM backup |
| Data keys | KMS | 90 days | KMS backup |
| API keys | Secrets Manager | 90 days | Secrets Manager |
| User keys | Client-side | Per user | User responsibility |

### Access Control

1. **Authentication:**

| Level | Method | MFA |
|-------|--------|-----|
| Public | None | N/A |
| Internal | SSO | Optional |
| Confidential | SSO | Required |
| Restricted | SSO + Hardware token | Required |

2. **Authorization:**

| Level | Model | Granularity |
|-------|-------|-------------|
| Public | None | N/A |
| Internal | RBAC | Role-based |
| Confidential | RBAC + ABAC | Role + attribute |
| Restricted | RBAC + ABAC + Context | Role + attribute + context |

### Monitoring

1. **Logging Requirements:**

| Level | Log Level | Retention | Alert |
|-------|-----------|-----------|-------|
| Public | Info | 30 days | No |
| Internal | Info | 90 days | No |
| Confidential | Debug | 1 year | Yes |
| Restricted | Trace | 7 years | Real-time |

2. **Monitoring Tools:**

| Tool | Purpose | Coverage |
|------|---------|----------|
| AWS CloudTrail | API logging | All AWS services |
| Datadog | Application monitoring | All apps |
| AWS GuardDuty | Threat detection | All infrastructure |
| DLP tools | Data loss prevention | All data stores |
| SIEM | Security events | All logs |

## 📋 Data Classification Process

### Step 1: Identify Data

1. Inventory all data assets
2. Map data flows
3. Identify data owners
4. Document data locations

### Step 2: Classify Data

1. Apply classification criteria
2. Assign classification level
3. Document classification rationale
4. Get data owner approval

### Step 3: Apply Controls

1. Implement appropriate controls
2. Configure access controls
3. Enable encryption
4. Set up monitoring

### Step 4: Review & Update

1. Quarterly classification review
2. Annual full audit
3. Update classifications as needed
4. Document changes

## 🚨 Data Breach Response

### Immediate Actions (0-1 hour)

1. **Contain the breach:**
   - Isolate affected systems
   - Revoke compromised credentials
   - Block malicious IPs
   - Preserve evidence

2. **Assess impact:**
   - Identify affected data
   - Determine classification level
   - Estimate number of records
   - Identify affected users

3. **Notify stakeholders:**
   - Security Team
   - Legal Team
   - Executive Team
   - PR Team

### Short-term Actions (1-24 hours)

1. **Investigate:**
   - Determine root cause
   - Identify attack vector
   - Document timeline
   - Preserve forensic evidence

2. **Remediate:**
   - Patch vulnerabilities
   - Reset credentials
   - Restore from backup
   - Verify system integrity

3. **Communicate:**
   - Notify affected users (if required)
   - Notify regulators (if required)
   - Update status page
   - Prepare press statement

### Long-term Actions (1-30 days)

1. **Post-incident review:**
   - Conduct blameless post-mortem
   - Identify lessons learned
   - Update policies
   - Implement improvements

2. **Regulatory compliance:**
   - Submit required reports
   - Cooperate with investigations
   - Implement corrective actions
   - Document compliance

## 📚 Compliance Mapping

| Control | GDPR | CCPA | PCI DSS | HIPAA | SOC 2 |
|---------|------|------|---------|-------|-------|
| Data Classification | Art. 30 | §1798.100 | 9.1 | 164.308 | CC6.1 |
| Access Control | Art. 32 | §1798.150 | 7.2 | 164.312 | CC6.2 |
| Encryption | Art. 32 | §1798.150 | 3.4 | 164.312 | CC6.1 |
| Monitoring | Art. 33 | §1798.150 | 10.2 | 164.312 | CC7.2 |
| Breach Notification | Art. 33 | §1798.150 | 12.1 | 164.308 | CC7.3 |

## 📞 Contact

**Policy Owner:** Privacy & Security Team  
**Data Protection Officer:** dpo@nidaw.com  
**Security Team:** security@nidaw.com  
**Emergency:** breach@nidaw.com (24/7)

---

**Last Updated:** July 2026  
**Version:** 1.0  
**Next Review:** October 2026  
**Classification:** Internal