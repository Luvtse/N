#!/usr/bin/env bash
# ============================================================================
# NIDAW Database Migration - Up
# Runs all pending database migrations
# ============================================================================

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() { echo -e "${BLUE}[INFO]${NC} $*"; }
log_success() { echo -e "${GREEN}[SUCCESS]${NC} $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*" >&2; }

ENVIRONMENT="${ENVIRONMENT:-development}"
MIGRATIONS_DIR="${MIGRATIONS_DIR:-$(dirname "$0")/../../database/migrations/postgres}"
DB_HOST="${DB_HOST:-localhost}"
DB_PORT="${DB_PORT:-5432}"
DB_NAME="${DB_NAME:-nidaw}"
DB_USER="${DB_USER:-nidaw}"
DB_PASSWORD="${DB_PASSWORD:-}"
TARGET_VERSION="${TARGET_VERSION:-}"
DRY_RUN="${DRY_RUN:-false}"

usage() {
    cat << EOF
Usage: $0 [OPTIONS]

Run database migrations up.

OPTIONS:
    -e, --environment ENV     Environment [default: development]
    -m, --migrations-dir DIR  Migrations directory
    --target VERSION          Target migration version
    --dry-run                 Show what would be done
    -h, --help                Show this help

EOF
    exit 0
}

while [[ $# -gt 0 ]]; do
    case $1 in
        -e|--environment) ENVIRONMENT="$2"; shift 2 ;;
        -m|--migrations-dir) MIGRATIONS_DIR="$2"; shift 2 ;;
        --target) TARGET_VERSION="$2"; shift 2 ;;
        --dry-run) DRY_RUN="true"; shift ;;
        -h|--help) usage ;;
        *) log_error "Unknown option: $1"; usage ;;
    esac
done

# Set database password
if [[ -n "$DB_PASSWORD" ]]; then
    export PGPASSWORD="$DB_PASSWORD"
fi

# Create migrations tracking table
create_migrations_table() {
    log_info "Creating migrations tracking table..."
    
    psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" << 'EOF'
CREATE TABLE IF NOT EXISTS schema_migrations (
    version VARCHAR(255) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    applied_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    checksum VARCHAR(64)
);
EOF
}

# Get current version
get_current_version() {
    psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -t -c \
        "SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1;" | tr -d ' '
}

# Get pending migrations
get_pending_migrations() {
    local current_version
    current_version=$(get_current_version)
    
    find "$MIGRATIONS_DIR" -name "*.sql" -type f | sort | while read -r migration; do
        local version
        version=$(basename "$migration" | cut -d'_' -f1)
        
        if [[ -z "$current_version" ]] || [[ "$version" > "$current_version" ]]; then
            echo "$migration"
        fi
    done
}

# Apply migration
apply_migration() {
    local migration_file="$1"
    local version
    version=$(basename "$migration_file" | cut -d'_' -f1)
    local name
    name=$(basename "$migration_file" .sql)
    local checksum
    checksum=$(sha256sum "$migration_file" | cut -d' ' -f1)
    
    log_info "Applying migration: $name"
    
    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "[DRY RUN] Would apply: $migration_file"
        return
    fi
    
    # Apply migration in transaction
    if ! psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" \
        --single-transaction --variable ON_ERROR_STOP=1 \
        -f "$migration_file"; then
        log_error "Migration failed: $name"
        exit 1
    fi
    
    # Record migration
    psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -c \
        "INSERT INTO schema_migrations (version, name, checksum) VALUES ('$version', '$name', '$checksum');"
    
    log_success "Migration applied: $name"
}

main() {
    log_info "Running database migrations up..."
    log_info "Environment: $ENVIRONMENT"
    log_info "Database: $DB_NAME @ $DB_HOST:$DB_PORT"
    
    create_migrations_table
    
    local current_version
    current_version=$(get_current_version)
    log_info "Current version: ${current_version:-none}"
    
    local pending_count=0
    get_pending_migrations | while read -r migration; do
        if [[ -n "$migration" ]]; then
            apply_migration "$migration"
            pending_count=$((pending_count + 1))
            
            if [[ -n "$TARGET_VERSION" ]]; then
                local version
                version=$(basename "$migration" | cut -d'_' -f1)
                if [[ "$version" == "$TARGET_VERSION" ]]; then
                    break
                fi
            fi
        fi
    done
    
    if [[ $pending_count -eq 0 ]]; then
        log_info "No pending migrations"
    else
        log_success "Migrations completed: $pending_count applied"
    fi
}

main "$@"