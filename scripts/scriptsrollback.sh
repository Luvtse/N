#!/usr/bin/env bash
# ============================================================================
# NIDAW Rollback Script
# Rolls back the last deployment to the previous version
# ============================================================================

set -euo pipefail

# ============================================================================
# CONFIGURATION
# ============================================================================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default values
ENVIRONMENT="${ENVIRONMENT:-production}"
NAMESPACE="${NAMESPACE:-nidaw-${ENVIRONMENT}}"
RELEASE_NAME="${RELEASE_NAME:-nidaw-${ENVIRONMENT}}"
REVISION="${REVISION:-}"  # Empty means rollback to previous
DRY_RUN="${DRY_RUN:-false}"
TIMEOUT="${TIMEOUT:-600s}"

# ============================================================================
# LOGGING FUNCTIONS
# ============================================================================

log_info() {
    echo -e "${BLUE}[INFO]${NC} $(date '+%Y-%m-%d %H:%M:%S') - $*"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $(date '+%Y-%m-%d %H:%M:%S') - $*"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $(date '+%Y-%m-%d %H:%M:%S') - $*"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $(date '+%Y-%m-%d %H:%M:%S') - $*"
}

# ============================================================================
# USAGE
# ============================================================================

usage() {
    cat << EOF
Usage: $0 [OPTIONS]

Rollback NIDAW deployment to a previous version.

OPTIONS:
    -e, --environment ENV    Environment (dev, staging, production) [default: production]
    -n, --namespace NS       Kubernetes namespace [default: nidaw-\${environment}]
    -r, --release NAME       Helm release name [default: nidaw-\${environment}]
    -v, --revision REV       Specific revision to rollback to [default: previous]
    -d, --dry-run            Show what would be done without making changes
    -t, --timeout TIME       Timeout for rollback operation [default: 600s]
    -h, --help               Show this help message

EXAMPLES:
    # Rollback to previous version
    $0

    # Rollback to specific revision
    $0 --revision 5

    # Rollback staging environment
    $0 --environment staging

    # Dry run
    $0 --dry-run

EOF
    exit 0
}

# ============================================================================
# ARGUMENT PARSING
# ============================================================================

while [[ $# -gt 0 ]]; do
    case $1 in
        -e|--environment)
            ENVIRONMENT="$2"
            shift 2
            ;;
        -n|--namespace)
            NAMESPACE="$2"
            shift 2
            ;;
        -r|--release)
            RELEASE_NAME="$2"
            shift 2
            ;;
        -v|--revision)
            REVISION="$2"
            shift 2
            ;;
        -d|--dry-run)
            DRY_RUN="true"
            shift
            ;;
        -t|--timeout)
            TIMEOUT="$2"
            shift 2
            ;;
        -h|--help)
            usage
            ;;
        *)
            log_error "Unknown option: $1"
            usage
            ;;
    esac
done

# ============================================================================
# PREREQUISITES CHECK
# ============================================================================

check_prerequisites() {
    log_info "Checking prerequisites..."
    
    # Check kubectl
    if ! command -v kubectl &> /dev/null; then
        log_error "kubectl is not installed"
        exit 1
    fi
    
    # Check helm
    if ! command -v helm &> /dev/null; then
        log_error "helm is not installed"
        exit 1
    fi
    
    # Check Kubernetes connection
    if ! kubectl cluster-info &> /dev/null; then
        log_error "Cannot connect to Kubernetes cluster"
        exit 1
    fi
    
    # Check namespace exists
    if ! kubectl get namespace "$NAMESPACE" &> /dev/null; then
        log_error "Namespace $NAMESPACE does not exist"
        exit 1
    fi
    
    # Check release exists
    if ! helm status "$RELEASE_NAME" -n "$NAMESPACE" &> /dev/null; then
        log_error "Helm release $RELEASE_NAME does not exist in namespace $NAMESPACE"
        exit 1
    fi
    
    log_success "All prerequisites met"
}

# ============================================================================
# GET CURRENT STATE
# ============================================================================

get_current_state() {
    log_info "Getting current deployment state..."
    
    # Get current revision
    CURRENT_REVISION=$(helm history "$RELEASE_NAME" -n "$NAMESPACE" --max 1 -q)
    log_info "Current revision: $CURRENT_REVISION"
    
    # Get current image tags
    CURRENT_BACKEND_IMAGE=$(kubectl get deployment -n "$NAMESPACE" -l app.kubernetes.io/component=backend -o jsonpath='{.items[0].spec.template.spec.containers[0].image}')
    CURRENT_FRONTEND_IMAGE=$(kubectl get deployment -n "$NAMESPACE" -l app.kubernetes.io/component=frontend -o jsonpath='{.items[0].spec.template.spec.containers[0].image}')
    
    log_info "Current backend image: $CURRENT_BACKEND_IMAGE"
    log_info "Current frontend image: $CURRENT_FRONTEND_IMAGE"
    
    # Get previous revision
    if [[ -z "$REVISION" ]]; then
        REVISION=$(helm history "$RELEASE_NAME" -n "$NAMESPACE" --max 2 -q | tail -n 1)
        if [[ "$REVISION" == "$CURRENT_REVISION" ]]; then
            log_error "No previous revision to rollback to"
            exit 1
        fi
    fi
    
    log_info "Target revision: $REVISION"
}

# ============================================================================
# CONFIRMATION
# ============================================================================

confirm_rollback() {
    if [[ "$DRY_RUN" == "true" ]]; then
        log_warning "DRY RUN MODE - No changes will be made"
        return
    fi
    
    echo ""
    echo "╔════════════════════════════════════════════════════════════╗"
    echo "║                    ROLLBACK SUMMARY                        ║"
    echo "╠════════════════════════════════════════════════════════════╣"
    echo "║ Environment:  $ENVIRONMENT"
    echo "║ Namespace:    $NAMESPACE"
    echo "║ Release:      $RELEASE_NAME"
    echo "║ Current:      Revision $CURRENT_REVISION"
    echo "║ Target:       Revision $REVISION"
    echo "╚════════════════════════════════════════════════════════════╝"
    echo ""
    
    read -p "Are you sure you want to proceed with rollback? (yes/no): " -r
    if [[ ! $REPLY =~ ^[Yy][Ee][Ss]$ ]]; then
        log_warning "Rollback cancelled by user"
        exit 0
    fi
}

# ============================================================================
# ROLLBACK
# ============================================================================

perform_rollback() {
    log_info "Initiating rollback to revision $REVISION..."
    
    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "[DRY RUN] Would execute: helm rollback $RELEASE_NAME $REVISION -n $NAMESPACE --timeout $TIMEOUT"
        return
    fi
    
    # Perform rollback
    if ! helm rollback "$RELEASE_NAME" "$REVISION" -n "$NAMESPACE" --timeout "$TIMEOUT" --wait; then
        log_error "Rollback failed"
        exit 1
    fi
    
    log_success "Rollback completed successfully"
}

# ============================================================================
# VERIFICATION
# ============================================================================

verify_rollback() {
    log_info "Verifying rollback..."
    
    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "[DRY RUN] Skipping verification"
        return
    fi
    
    # Wait for pods to be ready
    log_info "Waiting for pods to be ready..."
    if ! kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=nidaw -n "$NAMESPACE" --timeout=300s; then
        log_error "Pods did not become ready after rollback"
        exit 1
    fi
    
    # Check deployment status
    log_info "Checking deployment status..."
    kubectl get deployments -n "$NAMESPACE"
    
    # Get new image tags
    NEW_BACKEND_IMAGE=$(kubectl get deployment -n "$NAMESPACE" -l app.kubernetes.io/component=backend -o jsonpath='{.items[0].spec.template.spec.containers[0].image}')
    NEW_FRONTEND_IMAGE=$(kubectl get deployment -n "$NAMESPACE" -l app.kubernetes.io/component=frontend -o jsonpath='{.items[0].spec.template.spec.containers[0].image}')
    
    log_info "New backend image: $NEW_BACKEND_IMAGE"
    log_info "New frontend image: $NEW_FRONTEND_IMAGE"
    
    # Run health checks
    log_info "Running health checks..."
    if ! "${SCRIPT_DIR}/../scripts/smoke-test.sh" "${ENVIRONMENT}"; then
        log_warning "Smoke tests failed after rollback"
        log_warning "Manual investigation may be required"
    fi
    
    log_success "Rollback verification completed"
}

# ============================================================================
# NOTIFICATION
# ============================================================================

notify_stakeholders() {
    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "[DRY RUN] Skipping notifications"
        return
    fi
    
    log_info "Notifying stakeholders..."
    
    # Send Slack notification
    if [[ -n "${SLACK_WEBHOOK_URL:-}" ]]; then
        curl -X POST -H 'Content-type: application/json' \
            --data "{
                \"text\": \"🔄 NIDAW Rollback Completed\",
                \"blocks\": [
                    {
                        \"type\": \"section\",
                        \"text\": {
                            \"type\": \"mrkdwn\",
                            \"text\": \"*NIDAW Rollback Completed*\\nEnvironment: ${ENVIRONMENT}\\nRevision: ${CURRENT_REVISION} → ${REVISION}\\nTimestamp: $(date -u '+%Y-%m-%d %H:%M:%S UTC')\"
                        }
                    }
                ]
            }" \
            "$SLACK_WEBHOOK_URL" || log_warning "Failed to send Slack notification"
    fi
    
    # Update status page
    if [[ -n "${STATUS_PAGE_API_KEY:-}" ]]; then
        curl -X POST "https://status.nidaw.com/api/v1/incidents" \
            -H "Authorization: Bearer $STATUS_PAGE_API_KEY" \
            -H "Content-Type: application/json" \
            -d "{
                \"title\": \"Deployment Rollback\",
                \"status\": \"resolved\",
                \"message\": \"We have rolled back the deployment to revision ${REVISION} due to issues detected in revision ${CURRENT_REVISION}.\"
            }" || log_warning "Failed to update status page"
    fi
    
    log_success "Stakeholders notified"
}

# ============================================================================
# MAIN
# ============================================================================

main() {
    log_info "Starting NIDAW rollback process..."
    
    check_prerequisites
    get_current_state
    confirm_rollback
    perform_rollback
    verify_rollback
    notify_stakeholders
    
    log_success "Rollback process completed successfully!"
}

# Run main function
main "$@"