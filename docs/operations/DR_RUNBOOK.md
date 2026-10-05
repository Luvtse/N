# NIDAW Disaster Recovery Runbook

## Overview
This document outlines procedures for recovering from various disaster scenarios.

## RTO/RPO Targets
- **Recovery Time Objective (RTO)**: 15 minutes
- **Recovery Point Objective (RPO)**: 5 minutes

## Disaster Scenarios

### Scenario 1: Single Region Failure

**Detection:**
- CloudWatch alarms trigger
- Route53 health checks fail
- PagerDuty alert to on-call engineer

**Recovery Steps:**

1. **Assess Impact** (0-2 min)
   ```bash
   # Check region status
   aws ec2 describe-region-status --region us-east-1
   
   # Check RDS status
   aws rds describe-db-instances --region us-east-1
   
   # Check Kafka cluster
   kubectl get pods -n kafka --context us-east-1

2. Activate DR Region (2-5 min)

# Switch DNS to DR region
aws route53 change-resource-record-sets \
  --hosted-zone-id Z1234567890 \
  --change-batch file://dns-failover.json

# Scale up DR region
aws eks update-nodegroup-config \
  --cluster-name nidaw-prod-eu-west-1 \
  --nodegroup-name primary \
  --scaling-config desiredSize=15,maxSize=30

3. Verify Services (5-10 min)

# Check all services
kubectl get pods -n production --context eu-west-1

# Run smoke tests
./scripts/smoke-test.sh eu-west-1

# Check database connectivity
psql -h nidaw-postgres-dr.cluster-xxx.eu-west-1.rds.amazonaws.com \
     -U nidaw_admin -d nidaw -c "SELECT 1"
4. Monitor Recovery (10-15 min)

Watch Grafana dashboards
Monitor error rates
Verify data consistency
5. Communicate (ongoing)

Update status page
Notify stakeholders
Post incident report

Scenario 2: Database Corruption

Detection:
Application errors spike
Data integrity checks fail
Backup verification alerts
Recovery Steps:
1. Stop Writes (0-1 min)

# Enable read-only mode
kubectl patch configmap nidaw-config -n production \
  -p '{"data":{"DB_READ_ONLY":"true"}}'

# Restart backend pods
kubectl rollout restart deployment/nidaw-backend -n production

2. Identify Last Good Snapshot (1-3 min)

# List recent snapshots
aws rds describe-db-snapshots \
  --db-instance-identifier nidaw-postgres-prod \
  --query 'DBSnapshots[?Status==`available`].[DBSnapshotIdentifier,SnapshotCreateTime]' \
  --output table

3. Restore to New Instance (3-10 min)

# Restore from snapshot
aws rds restore-db-instance-from-db-snapshot \
  --db-instance-identifier nidaw-postgres-prod-restored \
  --db-snapshot-identifier nidaw-postgres-prod-daily-20260710-030000 \
  --db-instance-class db.r5.2xlarge \
  --multi-az

4. Point-in-Time Recovery (if needed)

# Restore to specific time
aws rds restore-db-instance-to-point-in-time \
  --source-db-instance-identifier nidaw-postgres-prod \
  --target-db-instance-identifier nidaw-postgres-prod-pitr \
  --restore-time 2026-07-10T14:30:00Z

5. Verify Data (10-12 min)

# Run data integrity checks
psql -h nidaw-postgres-prod-restored.xxx.rds.amazonaws.com \
     -U nidaw_admin -d nidaw \
     -f scripts/verify_data_integrity.sql

6. Cutover (12-15 min)

# Update connection string
aws secretsmanager update-secret \
  --secret-id nidaw/database-url \
  --secret-string '{"url":"postgresql://...restored..."}'

# Restart services
kubectl rollout restart deployment/nidaw-backend -n production

Scenario 3: Kafka Cluster Failure

Detection:
Event processing stops
Consumer lag increases
Application errors
Recovery Steps:

1. Assess Kafka Status (0-2 min)

# Check broker status
kubectl get pods -n kafka --context us-east-1

# Check topic status
kafka-topics --bootstrap-server kafka:9092 --describe

# Check consumer lag
kafka-consumer-groups --bootstrap-server kafka:9092 --describe --all-groups

2. Restore from Backup (2-10 min)

# Download latest backup
aws s3 cp s3://nidaw-backups-prod/kafka-backups/us-east-1/nidus.rides/latest.tar.gz /tmp/

# Restore topics
tar -xzf /tmp/latest.tar.gz -C /tmp/kafka-restore/

# Replay messages
kafka-console-producer --bootstrap-server kafka:9092 --topic nidus.rides < /tmp/kafka-restore/nidus.rides.messages

3. Verify Recovery (10-12 min)

# Check consumer lag is decreasing
watch -n 5 'kafka-consumer-groups --bootstrap-server kafka:9092 --describe --all-groups'

Scenario 4: Complete Data Center Loss
Recovery Steps:
1. Activate Secondary Region (0-5 min)
DNS failover already configured
Secondary region auto-scales
Services start handling traffic
2. Restore Data from Backups (5-30 min)

# Restore database from cross-region snapshot
aws rds restore-db-instance-from-db-snapshot \
  --region ap-southeast-1 \
  --db-instance-identifier nidaw-postgres-apac \
  --db-snapshot-identifier arn:aws:rds:us-east-1:123456789:snapshot:nidaw-postgres-prod-daily-20260710-030000

# Restore Redis from S3
aws s3 cp s3://nidaw-backups-prod/redis-backups/us-east-1/latest.rdb /tmp/

3. Rebuild Infrastructure (30-60 min)

# Deploy to new region
cd terraform/environments/prod
terraform apply -var="region=ap-southeast-1"

# Deploy application
helm upgrade --install nidaw ./helm/nidaw \
  --namespace production \
  --set global.region=ap-southeast-1

Communication Plan
Internal
0-5 min: Page on-call engineer
5-10 min: Notify engineering leadership
10-15 min: Notify executive team
15+ min: Regular updates every 30 min
External
0-15 min: Update status page (status.nidaw.com)
15-30 min: Email affected enterprise customers
30+ min: Social media updates
Post-Incident
Within 24 hours: Conduct blameless post-mortem
Within 48 hours: Publish incident report
Within 1 week: Implement preventive measures
Within 1 month: Update DR runbook based on learnings
Testing
Monthly: DR drill (tabletop exercise)
Quarterly: Full failover test
Annually: Complete data center loss simulation
Contacts
Primary On-Call: 
Engineering Lead: 
CTO: 
Support: 