#!/usr/bin/env bash
set -euo pipefail
# ============================================================================
# NIDAW Backup Restore Script
# Restores NIDAW data from backup
# ============================================================================

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() { echo -e "${BLUE}[INFO]${NC} $(date '+%Y-%m-%d %H:%M:%S') - $*"; }
log_success() { echo -e "${GREEN}[SUCCESS]${NC} $(date '+%Y-%m-%d %H:%M:%S') - $*"; }
log_warning() { echo -e "${YELLOW}[WARNING]${NC} $(date '+%Y-%m-%d %H:%M:%S') - $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $(date '+%Y-%m-%d %H:%M:%S') - $*" >&2; }

# Configuration
ENVIRONMENT="${ENVIRONMENT:-production}"
BACKUP_NAME="${BACKUP_NAME:-}"
BACKUP_DIR="${BACKUP_DIR:-/backups/nidaw/${ENVIRONMENT}}"
S3_BUCKET="${S3_BUCKET:-nidaw-backups}"
S3_REGION="${S3_REGION:-us-east-1}"

DB_HOST="${DB_HOST:-postgres}"
DB_PORT="${DB_PORT:-5432}"
DB_NAME="${DB_NAME:-nidaw}"
DB_USER="${DB_USER:-nidaw}"
DB_PASSWORD="${DB_PASSWORD:-}"

REDIS_HOST="${REDIS_HOST:-redis}"
REDIS_PORT="${REDIS_PORT:-6379}"

KAFKA_BROKERS="${KAFKA_BROKERS:-kafka:9092}"

ENCRYPTION_KEY="${ENCRYPTION_KEY:-}"
RESTORE_DB="true"
RESTORE_REDIS="true"
RESTORE_KAFKA="true"
DRY_RUN="false"

# Usage
usage() {
    cat << EOF
Usage: $0 [OPTIONS]

Restore NIDAW data from backup.

OPTIONS:
    -e, --environment ENV     Environment [default: production]
    -b, --backup-name NAME    Backup name to restore (required)
    -d, --backup-dir DIR      Local backup directory
    --from-s3                 Download backup from S3 first
    --db-only                 Restore only database
    --redis-only              Restore only Redis
    --kafka-only              Restore only Kafka
    --dry-run                 Show what would be done
    -h, --help                Show this help

EXAMPLES:
    $0 --backup-name nidaw-production-20260727-100000
    $0 --backup-name nidaw-production-20260727-100000 --from-s3
    $0 --backup-name nidaw-production-20260727-100000 --db-only

EOF
    exit 0
}

FROM_S3="false"

while [[ $# -gt 0 ]]; do
    case $1 in
        -e|--environment) ENVIRONMENT="$2"; shift 2 ;;
        -b|--backup-name) BACKUP_NAME="$2"; shift 2 ;;
        -d|--backup-dir) BACKUP_DIR="$2"; shift 2 ;;
        --from-s3) FROM_S3="true"; shift ;;
        --db-only) RESTORE_REDIS="false"; RESTORE_KAFKA="false"; shift ;;
        --redis-only) RESTORE_DB="false"; RESTORE_KAFKA="false"; shift ;;
        --kafka-only) RESTORE_DB="false"; RESTORE_REDIS="false"; shift ;;
        --dry-run) DRY_RUN="true"; shift ;;
        -h|--help) usage ;;
        *) log_error "Unknown option: $1"; usage ;;
    esac
done

if [[ -z "$BACKUP_NAME" ]]; then
    log_error "Backup name is required (--backup-name)"
    exit 1
fi

# Download from S3
download_from_s3() {
    if [[ "$FROM_S3" != "true" ]]; then
        return
    fi
    
    log_info "Downloading backup from S3..."
    
    local s3_path="s3://${S3_BUCKET}/backups/${ENVIRONMENT}/${BACKUP_NAME}"
    local local_path="${BACKUP_DIR}/${BACKUP_NAME}"
    
    mkdir -p "$local_path"
    
    if ! aws s3 sync "$s3_path" "$local_path" --region "$S3_REGION"; then
        log_error "Failed to download backup from S3"
        exit 1
    fi
    
    log_success "Backup downloaded from S3"
}

# Decrypt backup
decrypt_backup() {
    local encrypted_file="${BACKUP_DIR}/${BACKUP_NAME}.tar.gz.gpg"
    
    if [[ -f "$encrypted_file" ]]; then
        if [[ -z "$ENCRYPTION_KEY" ]]; then
            log_error "Backup is encrypted but no encryption key provided"
            exit 1
        fi
        
        log_info "Decrypting backup..."
        
        gpg --batch --yes --passphrase "$ENCRYPTION_KEY" \
            --output "${BACKUP_DIR}/${BACKUP_NAME}.tar.gz" "$encrypted_file"
        
        tar -xzf "${BACKUP_DIR}/${BACKUP_NAME}.tar.gz" -C "${BACKUP_DIR}"
        
        log_success "Backup decrypted"
    fi
}

# Restore database
restore_database() {
    if [[ "$RESTORE_DB" != "true" ]]; then
        log_info "Skipping database restore"
        return
    fi
    
    log_info "Restoring PostgreSQL database..."
    
    local db_backup="${BACKUP_DIR}/${BACKUP_NAME}/database.sql.gz"
    
    if [[ ! -f "$db_backup" ]]; then
        log_warning "Database backup not found, skipping"
        return
    fi
    
    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "[DRY RUN] Would restore database from $db_backup"
        return
    fi
    
    # Stop application writes
    log_warning "Stopping application writes..."
    kubectl scale deployment/nidaw-backend --replicas=0 -n "nidaw-${ENVIRONMENT}" || true
    
    # Drop and recreate database
    if [[ -n "$DB_PASSWORD" ]]; then
        export PGPASSWORD="$DB_PASSWORD"
    fi
    
    psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d postgres -c "DROP DATABASE IF EXISTS ${DB_NAME};"
    psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d postgres -c "CREATE DATABASE ${DB_NAME};"
    
    # Restore from backup
    if ! pg_restore -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" \
        --verbose --clean --if-exists "$db_backup" 2>&1 | tee "${BACKUP_DIR}/${BACKUP_NAME}/db-restore.log"; then
        log_error "Database restore failed"
        exit 1
    fi
    
    # Restart application
    log_info "Restarting application..."
    kubectl scale deployment/nidaw-backend --replicas=5 -n "nidaw-${ENVIRONMENT}" || true
    
    log_success "Database restore completed"
}

# Restore Redis
restore_redis() {
    if [[ "$RESTORE_REDIS" != "true" ]]; then
        log_info "Skipping Redis restore"
        return
    fi
    
    log_info "Restoring Redis..."
    
    local redis_backup="${BACKUP_DIR}/${BACKUP_NAME}/redis.rdb.gz"
    
    if [[ ! -f "$redis_backup" ]]; then
        log_warning "Redis backup not found, skipping"
        return
    fi
    
    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "[DRY RUN] Would restore Redis from $redis_backup"
        return
    fi
    
    # Decompress
    gunzip -c "$redis_backup" > /tmp/redis.rdb
    
    # Stop Redis
    redis-cli -h "$REDIS_HOST" -p "$REDIS_PORT" SHUTDOWN NOSAVE || true
    sleep 2
    
    # Copy RDB file
    cp /tmp/redis.rdb /var/lib/redis/dump.rdb
    chown redis:redis /var/lib/redis/dump.rdb
    
    # Start Redis
    redis-server --daemonize yes
    
    log_success "Redis restore completed"
}

# Restore Kafka
restore_kafka() {
    if [[ "$RESTORE_KAFKA" != "true" ]]; then
        log_info "Skipping Kafka restore"
        return
    fi
    
    log_info "Restoring Kafka topics..."
    
    local kafka_backup_dir="${BACKUP_DIR}/${BACKUP_NAME}/kafka"
    
    if [[ ! -d "$kafka_backup_dir" ]]; then
        log_warning "Kafka backup not found, skipping"
        return
    fi
    
    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "[DRY RUN] Would restore Kafka topics from $kafka_backup_dir"
        return
    fi
    
    for backup_file in "$kafka_backup_dir"/*.json.gz; do
        if [[ -f "$backup_file" ]]; then
            local topic
            topic=$(basename "$backup_file" .json.gz)
            
            log_info "Restoring topic: $topic"
            
            # Create topic if not exists
            kafka-topics --create --bootstrap-server "$KAFKA_BROKERS" \
                --topic "$topic" --partitions 6 --replication-factor 3 --if-not-exists || true
            
            # Produce messages
            gunzip -c "$backup_file" | kafka-console-producer \
                --bootstrap-server "$KAFKA_BROKERS" \
                --topic "$topic"
            
            log_success "Topic $topic restored"
        fi
    done
    
    log_success "Kafka restore completed"
}

# Main
main() {
    log_info "Starting NIDAW backup restore..."
    log_info "Backup: $BACKUP_NAME"
    
    if [[ "$DRY_RUN" == "true" ]]; then
        log_warning "DRY RUN MODE - No changes will be made"
    fi
    
    download_from_s3
    decrypt_backup
    restore_database
    restore_redis
    restore_kafka
    
    log_success "Restore completed successfully!"
}

main "$@"
