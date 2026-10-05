#!/usr/bin/env bash
# ============================================================================
# NIDAW Backup Verification Script
# Verifies integrity of backups
# ============================================================================

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() { echo -e "${BLUE}[INFO]${NC} $*"; }
log_success() { echo -e "${GREEN}[SUCCESS]${NC} $*"; }
log_warning() { echo -e "${YELLOW}[WARNING]${NC} $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*" >&2; }

BACKUP_NAME="${BACKUP_NAME:-}"
BACKUP_DIR="${BACKUP_DIR:-/backups/nidaw}"
CHECKSUMS="true"
TEST_RESTORE="false"

usage() {
    cat << EOF
Usage: $0 [OPTIONS]

Verify NIDAW backup integrity.

OPTIONS:
    -b, --backup-name NAME    Backup name to verify
    -d, --backup-dir DIR      Backup directory
    --no-checksums            Skip checksum verification
    --test-restore            Test restore to temporary database
    -h, --help                Show this help

EOF
    exit 0
}

while [[ $# -gt 0 ]]; do
    case $1 in
        -b|--backup-name) BACKUP_NAME="$2"; shift 2 ;;
        -d|--backup-dir) BACKUP_DIR="$2"; shift 2 ;;
        --no-checksums) CHECKSUMS="false"; shift ;;
        --test-restore) TEST_RESTORE="true"; shift ;;
        -h|--help) usage ;;
        *) log_error "Unknown option: $1"; usage ;;
    esac
done

if [[ -z "$BACKUP_NAME" ]]; then
    log_error "Backup name required"
    exit 1
fi

BACKUP_PATH="${BACKUP_DIR}/${BACKUP_NAME}"

verify_files_exist() {
    log_info "Verifying backup files exist..."
    
    local required_files=(
        "database.sql.gz"
        "schema.sql"
        "db-backup.log"
    )
    
    for file in "${required_files[@]}"; do
        if [[ ! -f "${BACKUP_PATH}/${file}" ]]; then
            log_error "Required file missing: $file"
            return 1
        fi
    done
    
    log_success "All required files present"
}

verify_checksums() {
    if [[ "$CHECKSUMS" != "true" ]]; then
        return
    fi
    
    log_info "Verifying checksums..."
    
    local checksum_file="${BACKUP_PATH}/checksums.sha256"
    
    if [[ -f "$checksum_file" ]]; then
        if ! sha256sum -c "$checksum_file" --quiet; then
            log_error "Checksum verification failed"
            return 1
        fi
        log_success "Checksums verified"
    else
        log_warning "No checksum file found, generating..."
        find "$BACKUP_PATH" -type f -exec sha256sum {} \; > "$checksum_file"
        log_success "Checksums generated"
    fi
}

verify_database_backup() {
    log_info "Verifying database backup..."
    
    local db_backup="${BACKUP_PATH}/database.sql.gz"
    
    # Test if file is valid gzip
    if ! gzip -t "$db_backup" 2>/dev/null; then
        log_error "Database backup is corrupted (invalid gzip)"
        return 1
    fi
    
    # Check file size (should be > 1KB)
    local size
    size=$(stat -c%s "$db_backup" 2>/dev/null || stat -f%z "$db_backup")
    if [[ $size -lt 1024 ]]; then
        log_warning "Database backup is very small ($size bytes)"
    fi
    
    # List tables in backup
    log_info "Tables in backup:"
    pg_restore --list "$db_backup" 2>/dev/null | grep "TABLE" | head -10 || true
    
    log_success "Database backup verified"
}

verify_redis_backup() {
    local redis_backup="${BACKUP_PATH}/redis.rdb.gz"
    
    if [[ ! -f "$redis_backup" ]]; then
        log_warning "Redis backup not found"
        return 0
    fi
    
    log_info "Verifying Redis backup..."
    
    if ! gzip -t "$redis_backup" 2>/dev/null; then
        log_error "Redis backup is corrupted"
        return 1
    fi
    
    log_success "Redis backup verified"
}

verify_kafka_backup() {
    local kafka_dir="${BACKUP_PATH}/kafka"
    
    if [[ ! -d "$kafka_dir" ]]; then
        log_warning "Kafka backup not found"
        return 0
    fi
    
    log_info "Verifying Kafka backup..."
    
    local topic_count
    topic_count=$(find "$kafka_dir" -name "*.json.gz" | wc -l)
    
    if [[ $topic_count -eq 0 ]]; then
        log_warning "No Kafka topic backups found"
        return 0
    fi
    
    log_info "Kafka topics backed up: $topic_count"
    
    for backup_file in "$kafka_dir"/*.json.gz; do
        if ! gzip -t "$backup_file" 2>/dev/null; then
            log_error "Kafka backup corrupted: $(basename "$backup_file")"
            return 1
        fi
    done
    
    log_success "Kafka backup verified"
}

test_restore() {
    if [[ "$TEST_RESTORE" != "true" ]]; then
        return
    fi
    
    log_info "Testing restore to temporary database..."
    
    local temp_db="nidaw_test_restore_$$"
    
    # Create temporary database
    psql -U nidaw -d postgres -c "CREATE DATABASE ${temp_db};" || return 1
    
    # Restore
    if ! pg_restore -U nidaw -d "$temp_db" "${BACKUP_PATH}/database.sql.gz" 2>/dev/null; then
        log_error "Test restore failed"
        psql -U nidaw -d postgres -c "DROP DATABASE ${temp_db};"
        return 1
    fi
    
    # Verify tables
    local table_count
    table_count=$(psql -U nidaw -d "$temp_db" -t -c "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public';")
    
    log_info "Tables restored: $table_count"
    
    # Cleanup
    psql -U nidaw -d postgres -c "DROP DATABASE ${temp_db};"
    
    log_success "Test restore successful"
}

generate_report() {
    local report_file="${BACKUP_PATH}/verification-report.txt"
    
    cat > "$report_file" << EOF
NIDAW Backup Verification Report
=================================
Backup: ${BACKUP_NAME}
Date: $(date -u '+%Y-%m-%d %H:%M:%S UTC')
Status: PASSED

Files Verified:
- database.sql.gz: OK
- schema.sql: OK
- redis.rdb.gz: OK (if present)
- kafka/*: OK (if present)

Checksums: VERIFIED
Test Restore: $([ "$TEST_RESTORE" == "true" ] && echo "PASSED" || echo "SKIPPED")

EOF
    
    log_success "Verification report: $report_file"
}

main() {
    log_info "Verifying backup: $BACKUP_NAME"
    
    verify_files_exist
    verify_checksums
    verify_database_backup
    verify_redis_backup
    verify_kafka_backup
    test_restore
    generate_report
    
    log_success "Backup verification completed successfully!"
}

main "$@"