#!/usr/bin/env bash
# ============================================================================
# NIDAW Database Migration - Down
# Rolls back database migrations
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

ENVIRONMENT="${ENVIRONMENT:-development}"
MIGRATIONS_DIR="${MIGRATIONS_DIR:-$(dirname "$0")/../../database/migrations/postgres}"
DB_HOST="${DB_HOST:-localhost}"
DB_PORT="${DB_PORT:-5432}"
DB_NAME="${DB_NAME:-nidaw}"
DB_USER="${DB_USER:-nidaw}"
DB_PASSWORD="${DB_PASSWORD:-}"
STEPS="${STEPS:-1}"
FORCE="false"

if [[ -n "$DB_PASSWORD" ]]; then
    export PGPASSWORD="$DB_PASSWORD"
fi

usage() {
    cat << EOF
Usage: $0 [OPTIONS]

Rollback database migrations.

OPTIONS:
    -e, --environment ENV     Environment [default: development]
    -s, --steps N             Number of migrations to rollback [default: 1]
    --force                   Skip confirmation
    -h, --help                Show this help

WARNING: Rolling back migrations can result in data loss!

EOF
    exit 0
}

while [[ $# -gt 0 ]]; do
    case $1 in
        -e|--environment) ENVIRONMENT="$2"; shift 2 ;;
        -s|--steps) STEPS="$2"; shift 2 ;;
        --force) FORCE="true"; shift ;;
        -h|--help) usage ;;
        *) log_error "Unknown option: $1"; usage ;;
    esac
done

get_applied_migrations() {
    psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -t -c \
        "SELECT version, name FROM schema_migrations ORDER BY version DESC LIMIT $STEPS;"
}

rollback_migration() {
    local version="$1"
    local name="$2"
    
    log_warning "Rolling back migration: $name (version: $version)"
    
    # Look for down migration file
    local down_file
    down_file=$(find "$MIGRATIONS_DIR" -name "*_${version}_down.sql" -o -name "${version}_down.sql" | head -1)
    
    if [[ -z "$down_file" ]]; then
        log_error "No down migration found for version $version"
        log_error "Manual intervention required"
        exit 1
    fi
    
    log_info "Applying down migration: $down_file"
    
    if ! psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" \
        --single-transaction --variable ON_ERROR_STOP=1 \
        -f "$down_file"; then
        log_error "Rollback failed: $name"
        exit 1
    fi
    
    # Remove from tracking
    psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -c \
        "DELETE FROM schema_migrations WHERE version = '$version';"
    
    log_success "Rolled back: $name"
}

main() {
    log_warning "DATABASE MIGRATION ROLLBACK"
    log_warning "This operation may result in data loss!"
    
    if [[ "$FORCE" != "true" ]]; then
        read -p "Are you sure you want to rollback $STEPS migration(s)? (yes/no): " -r
        if [[ ! $REPLY =~ ^[Yy][Ee][Ss]$ ]]; then
            log_info "Rollback cancelled"
            exit 0
        fi
    fi
    
    log_info "Rolling back $STEPS migration(s)..."
    
    get_applied_migrations | while read -r line; do
        if [[ -n "$line" ]]; then
            local version name
            version=$(echo "$line" | cut -d'|' -f1 | tr -d ' ')
            name=$(echo "$line" | cut -d'|' -f2 | tr -d ' ')
            
            if [[ -n "$version" && -n "$name" ]]; then
                rollback_migration "$version" "$name"
            fi
        fi
    done
    
    log_success "Rollback completed"
}

main "$@"