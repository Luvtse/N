#!/usr/bin/env bash
# ============================================================================
# NIDAW Daily Backup Script
# Creates daily backups of PostgreSQL, Redis, and Kafka
# ============================================================================

set -euo pipefail

# ============================================================================
# CONFIGURATION
# ============================================================================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$(dirname "$SCRIPT_DIR")")"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Backup configuration
ENVIRONMENT="${ENVIRONMENT:-production}"
BACKUP_DIR="${BACKUP_DIR:-/backups/nidaw/${ENVIRONMENT}}"
RETENTION_DAYS="${RETENTION_DAYS:-35}"
TIMESTAMP="$(date '+%Y%m%d-%H%M%S')"
BACKUP_NAME="nidaw-${ENVIRONMENT}-${TIMESTAMP}"

# Database configuration
DB_HOST="${DB_HOST:-postgres}"
DB_PORT="${DB_PORT:-5432}"
DB_NAME="${DB_NAME:-nidaw}"
DB_USER="${DB_USER:-nidaw}"
DB_PASSWORD="${DB_PASSWORD:-}"

# Redis configuration
REDIS_HOST="${REDIS_HOST:-redis}"
REDIS_PORT="${REDIS_PORT:-6379}"

# Kafka configuration
KAFKA_BROKERS="${KAFKA_BROKERS:-kafka:9092}"
KAFKA_TOPICS="${KAFKA_TOPICS:-nidus.rides,nidus.locations,haven.bookings,vorax.orders,payments}"

# S3 configuration
S3_BUCKET="${S3_BUCKET:-nidaw-backups}"
S3_REGION="${S3_REGION:-us-east-1}"
S3_PREFIX="backups/${ENVIRONMENT}/${TIMESTAMP}"

# Encryption
ENCRYPTION_KEY="${ENCRYPTION_KEY:-}"
ENCRYPT_BACKUPS="${ENCRYPT_BACKUPS:-true}"

# ============================================================================
# LOGGING
# ============================================================================

log_info() { echo -e "${BLUE}[INFO]${NC} $(date '+%Y-%m-%d %H:%M:%S') - $*"; }
log_success() { echo -e "${GREEN}[SUCCESS]${NC} $(date '+%Y-%m-%d %H:%M:%S') - $*"; }
log_warning() { echo -e "${YELLOW}[WARNING]${NC} $(date '+%Y-%m-%d %H:%M:%S') - $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $(date '+%Y-%m-%d %H:%M:%S') - $*" >&2; }

# ============================================================================
# USAGE
# ============================================================================

usage() {
    cat << EOF
Usage: $0 [OPTIONS]

Create daily backups of NIDAW data stores.

OPTIONS:
    -e, --environment ENV    Environment (dev, staging, production) [default: production]
    -d, --backup-dir DIR     Local backup directory [default: /backups/nidaw/\${env}]
    -r, --retention DAYS     Retention period in days [default: 35]
    --no-s3                  Skip S3 upload
    --no-encrypt             Skip encryption
    --db-only                Backup only database
    --redis-only             Backup only Redis
    --kafka-only             Backup only Kafka
    -h, --help               Show this help message

EOF
    exit 0
}

# Parse arguments
UPLOAD_TO_S3="true"
BACKUP_DB="true"
BACKUP_REDIS="true"
BACKUP_KAFKA="true"

while [[ $# -gt 0 ]]; do
    case $1 in
        -e|--environment) ENVIRONMENT="$2"; shift 2 ;;
        -d|--backup-dir) BACKUP_DIR="$2"; shift 2 ;;
        -r|--retention) RETENTION_DAYS="$2"; shift 2 ;;
        --no-s3) UPLOAD_TO_S3="false"; shift ;;
        --no-encrypt) ENCRYPT_BACKUPS="false"; shift ;;
        --db-only) BACKUP_REDIS="false"; BACKUP_KAFKA="false"; shift ;;
        --redis-only) BACKUP_DB="false"; BACKUP_KAFKA="false"; shift ;;
        --kafka-only) BACKUP_DB="false"; BACKUP_REDIS="false"; shift ;;
        -h|--help) usage ;;
        *) log_error "Unknown option: $1"; usage ;;
    esac
done

# ============================================================================
# SETUP
# ============================================================================

setup() {
    log_info "Setting up backup environment..."
    
    # Create backup directory
    mkdir -p "${BACKUP_DIR}/${BACKUP_NAME}"
    
    # Check prerequisites
    command -v pg_dump &> /dev/null || { log_error "pg_dump not found"; exit 1; }
    command -v redis-cli &> /dev/null || { log_error "redis-cli not found"; exit 1; }
    command -v aws &> /dev/null || { log_error "aws cli not found"; exit 1; }
    
    # Set PostgreSQL password
    if [[ -n "$DB_PASSWORD" ]]; then
        export PGPASSWORD="$DB_PASSWORD"
    fi
    
    log_success "Backup environment ready"
}

# ============================================================================
# DATABASE BACKUP
# ============================================================================

backup_database() {
    if [[ "$BACKUP_DB" != "true" ]]; then
        log_info "Skipping database backup"
        return
    fi
    
    log_info "Backing up PostgreSQL database..."
    
    local db_backup_file="${BACKUP_DIR}/${BACKUP_NAME}/database.sql.gz"
    
    # Create full database dump
    if ! pg_dump -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" \
        --format=custom --compress=9 --verbose \
        --file="$db_backup_file" 2>&1 | tee "${BACKUP_DIR}/${BACKUP_NAME}/db-backup.log"; then
        log_error "Database backup failed"
        return 1
    fi
    
    # Create schema-only backup
    pg_dump -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" \
        --schema-only --file="${BACKUP_DIR}/${BACKUP_NAME}/schema.sql"
    
    # Get database size
    local db_size
    db_size=$(du -h "$db_backup_file" | cut -f1)
    
    log_success "Database backup completed: $db_size"
}

# ============================================================================
# REDIS BACKUP
# ============================================================================

backup_redis() {
    if [[ "$BACKUP_REDIS" != "true" ]]; then
        log_info "Skipping Redis backup"
        return
    fi
    
    log_info "Backing up Redis..."
    
    local redis_backup_file="${BACKUP_DIR}/${BACKUP_NAME}/redis.rdb"
    
    # Trigger BGSAVE
    redis-cli -h "$REDIS_HOST" -p "$REDIS_PORT" BGSAVE
    
    # Wait for save to complete
    local timeout=60
    local elapsed=0
    while [[ $elapsed -lt $timeout ]]; do
        local lastsave
        lastsave=$(redis-cli -h "$REDIS_HOST" -p "$REDIS_PORT" LASTSAVE)
        sleep 2
        elapsed=$((elapsed + 2))
        
        # Check if save completed (simplified check)
        if redis-cli -h "$REDIS_HOST" -p "$REDIS_PORT" INFO persistence | grep -q "rdb_bgsave_in_progress:0"; then
            break
        fi
    done
    
    # Copy RDB file
    if [[ -f "/var/lib/redis/dump.rdb" ]]; then
        cp "/var/lib/redis/dump.rdb" "$redis_backup_file"
        gzip "$redis_backup_file"
        log_success "Redis backup completed"
    else
        log_warning "Redis RDB file not found, using redis-cli dump"
        redis-cli -h "$REDIS_HOST" -p "$REDIS_PORT" --rdb "$redis_backup_file"
        gzip "$redis_backup_file"
    fi
}

# ============================================================================
# KAFKA BACKUP
# ============================================================================

backup_kafka() {
    if [[ "$BACKUP_KAFKA" != "true" ]]; then
        log_info "Skipping Kafka backup"
        return
    fi
    
    log_info "Backing up Kafka topics..."
    
    local kafka_backup_dir="${BACKUP_DIR}/${BACKUP_NAME}/kafka"
    mkdir -p "$kafka_backup_dir"
    
    IFS=',' read -ra TOPICS <<< "$KAFKA_TOPICS"
    
    for topic in "${TOPICS[@]}"; do
        log_info "Backing up topic: $topic"
        
        local topic_backup="${kafka_backup_dir}/${topic}.json.gz"
        
        # Consume all messages from topic
        if timeout 3600 kafka-console-consumer \
            --bootstrap-server "$KAFKA_BROKERS" \
            --topic "$topic" \
            --from-beginning \
            --timeout-ms 60000 \
            --property print.key=true \
            --property print.timestamp=true \
            2>/dev/null | gzip > "$topic_backup"; then
            
            local size
            size=$(du -h "$topic_backup" | cut -f1)
            log_success "Topic $topic backed up: $size"
        else
            log_warning "Kafka backup for topic $topic may be incomplete"
        fi
    done
    
    # Backup consumer group offsets
    kafka-consumer-groups --bootstrap-server "$KAFKA_BROKERS" \
        --describe --all-groups > "${kafka_backup_dir}/consumer-groups.txt"
    
    log_success "Kafka backup completed"
}

# ============================================================================
# ENCRYPTION
# ============================================================================

encrypt_backups() {
    if [[ "$ENCRYPT_BACKUPS" != "true" ]]; then
        log_info "Skipping encryption"
        return
    fi
    
    if [[ -z "$ENCRYPTION_KEY" ]]; then
        log_warning "No encryption key provided, skipping encryption"
        return
    fi
    
    log_info "Encrypting backups..."
    
    # Create encrypted archive
    local archive_file="${BACKUP_DIR}/${BACKUP_NAME}.tar.gz"
    tar -czf "$archive_file" -C "${BACKUP_DIR}" "${BACKUP_NAME}"
    
    # Encrypt with GPG
    if command -v gpg &> /dev/null; then
        gpg --batch --yes --passphrase "$ENCRYPTION_KEY" \
            --cipher-algo AES256 --symmetric \
            --output "${archive_file}.gpg" "$archive_file"
        
        rm "$archive_file"
        log_success "Backups encrypted: ${archive_file}.gpg"
    else
        log_warning "GPG not found, skipping encryption"
    fi
}

# ============================================================================
# S3 UPLOAD
# ============================================================================

upload_to_s3() {
    if [[ "$UPLOAD_TO_S3" != "true" ]]; then
        log_info "Skipping S3 upload"
        return
    fi
    
    log_info "Uploading backups to S3..."
    
    local s3_path="s3://${S3_BUCKET}/${S3_PREFIX}"
    
    # Upload all backup files
    if ! aws s3 sync "${BACKUP_DIR}/${BACKUP_NAME}" "$s3_path" \
        --region "$S3_REGION" \
        --storage-class STANDARD_IA \
        --sse AES256; then
        log_error "S3 upload failed"
        return 1
    fi
    
    log_success "Backups uploaded to S3: $s3_path"
}

# ============================================================================
# CLEANUP
# ============================================================================

cleanup_old_backups() {
    log_info "Cleaning up backups older than $RETENTION_DAYS days..."
    
    # Local cleanup
    find "$BACKUP_DIR" -maxdepth 1 -type d -name "nidaw-*" -mtime +${RETENTION_DAYS} -exec rm -rf {} \;
    
    # S3 cleanup
    if [[ "$UPLOAD_TO_S3" == "true" ]]; then
        aws s3 ls "s3://${S3_BUCKET}/backups/${ENVIRONMENT}/" --region "$S3_REGION" | \
            awk '{print $4}' | while read -r prefix; do
                local backup_date
                backup_date=$(echo "$prefix" | grep -oP '\d{8}-\d{6}')
                if [[ -n "$backup_date" ]]; then
                    local backup_timestamp
                    backup_timestamp=$(date -d "${backup_date:0:8} ${backup_date:9:2}:${backup_date:11:2}:${backup_date:13:2}" +%s)
                    local cutoff_timestamp
                    cutoff_timestamp=$(date -d "-${RETENTION_DAYS} days" +%s)
                    
                    if [[ $backup_timestamp -lt $cutoff_timestamp ]]; then
                        aws s3 rm "s3://${S3_BUCKET}/backups/${ENVIRONMENT}/${prefix}" --recursive --region "$S3_REGION"
                        log_info "Deleted old backup: $prefix"
                    fi
                fi
            done
    fi
    
    log_success "Cleanup completed"
}

# ============================================================================
# VERIFICATION
# ============================================================================

verify_backup() {
    log_info "Verifying backup integrity..."
    
    # Run verification script
    if "${SCRIPT_DIR}/verify-backup.sh" --backup-name "$BACKUP_NAME"; then
        log_success "Backup verification passed"
    else
        log_error "Backup verification failed"
        return 1
    fi
}

# ============================================================================
# NOTIFICATION
# ============================================================================

notify_completion() {
    log_info "Sending backup completion notification..."
    
    local backup_size
    backup_size=$(du -sh "${BACKUP_DIR}/${BACKUP_NAME}" | cut -f1)
    
    # Slack notification
    if [[ -n "${SLACK_WEBHOOK_URL:-}" ]]; then
        curl -X POST -H 'Content-type: application/json' \
            --data "{
                \"text\": \"✅ NIDAW Daily Backup Completed\",
                \"blocks\": [
                    {
                        \"type\": \"section\",
                        \"text\": {
                            \"type\": \"mrkdwn\",
                            \"text\": \"*Daily Backup Completed*\\nEnvironment: ${ENVIRONMENT}\\nBackup: ${BACKUP_NAME}\\nSize: ${backup_size}\\nTimestamp: $(date -u '+%Y-%m-%d %H:%M:%S UTC')\"
                        }
                    }
                ]
            }" \
            "$SLACK_WEBHOOK_URL" || log_warning "Failed to send Slack notification"
    fi
}

# ============================================================================
# MAIN
# ============================================================================

main() {
    log_info "Starting NIDAW daily backup..."
    log_info "Backup name: $BACKUP_NAME"
    
    local start_time
    start_time=$(date +%s)
    
    setup
    backup_database
    backup_redis
    backup_kafka
    encrypt_backups
    upload_to_s3
    verify_backup
    cleanup_old_backups
    notify_completion
    
    local end_time
    end_time=$(date +%s)
    local duration=$((end_time - start_time))
    
    log_success "Daily backup completed in ${duration}s"
    log_success "Backup location: ${BACKUP_DIR}/${BACKUP_NAME}"
}

# Run main function
main "$@"