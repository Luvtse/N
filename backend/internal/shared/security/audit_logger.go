package security

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// SOC 2 requires comprehensive audit logging for:
// - Access control (CC6.1, CC6.2, CC6.3)
// - System operations (CC7.1, CC7.2)
// - Change management (CC8.1)
// - Risk mitigation (CC9.1)

type AuditEvent struct {
	ID            string                 `json:"id"`
	Timestamp     time.Time              `json:"timestamp"`
	EventType     string                 `json:"event_type"`
	Actor         AuditActor             `json:"actor"`
	Resource      AuditResource          `json:"resource"`
	Action        string                 `json:"action"`
	Result        string                 `json:"result"` // success, failure, denied
	IPAddress     string                 `json:"ip_address"`
	UserAgent     string                 `json:"user_agent"`
	RequestId     string                 `json:"request_id"`
	SessionId     string                 `json:"session_id"`
	Metadata      map[string]interface{} `json:"metadata"`
	Duration      time.Duration          `json:"duration"`
	ComplianceTag []string               `json:"compliance_tag"` // SOC2, GDPR, PCI, HIPAA
}

type AuditActor struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // user, service, system
	Role     string `json:"role"`
	TenantID string `json:"tenant_id"`
}

type AuditResource struct {
	Type string `json:"type"` // ride, payment, user, booking
	ID   string `json:"id"`
}

type AuditLogger struct {
	buffer    chan AuditEvent
	batchSize int
	flushInterval time.Duration
}

func NewAuditLogger(bufferSize int, batchSize int, flushInterval time.Duration) *AuditLogger {
	logger := &AuditLogger{
		buffer:        make(chan AuditEvent, bufferSize),
		batchSize:     batchSize,
		flushInterval: flushInterval,
	}
	
	go logger.flushLoop()
	return logger
}

func (l *AuditLogger) Log(ctx context.Context, event AuditEvent) {
	if event.ID == "" {
		event.ID = uuid.New().String()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	
	select {
	case l.buffer <- event:
	default:
		// Buffer full, log directly (fallback)
		l.writeEvent(event)
	}
}

func (l *AuditLogger) flushLoop() {
	ticker := time.NewTicker(l.flushInterval)
	defer ticker.Stop()
	
	batch := make([]AuditEvent, 0, l.batchSize)
	
	for {
		select {
		case event := <-l.buffer:
			batch = append(batch, event)
			if len(batch) >= l.batchSize {
				l.writeBatch(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				l.writeBatch(batch)
				batch = batch[:0]
			}
		}
	}
}

func (l *AuditLogger) writeBatch(events []AuditEvent) {
	// Write to audit log table (append-only)
	// In production: write to dedicated audit DB + S3 + SIEM
	for _, event := range events {
		l.writeEvent(event)
	}
}

func (l *AuditLogger) writeEvent(event AuditEvent) {
	data, _ := json.Marshal(event)
	// Write to audit_logs table (immutable)
	// INSERT INTO audit_logs (id, timestamp, event_type, actor_id, resource_type, resource_id, action, result, ip_address, event_data) VALUES (...)
	_ = data
}

// AuditMiddleware captures HTTP requests for audit
type AuditMiddleware struct {
	logger *AuditLogger
}

func NewAuditMiddleware(logger *AuditLogger) *AuditMiddleware {
	return &AuditMiddleware{logger: logger}
}

func (m *AuditMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		requestID := uuid.New().String()
		
		// Wrap response writer to capture status
		wrapped := &responseWriter{ResponseWriter: w, statusCode: 200}
		
		next.ServeHTTP(wrapped, r)
		
		// Determine result
		result := "success"
		if wrapped.statusCode >= 400 {
			result = "failure"
		}
		if wrapped.statusCode == 401 || wrapped.statusCode == 403 {
			result = "denied"
		}
		
		// Log audit event
		m.logger.Log(r.Context(), AuditEvent{
			EventType: "http_request",
			Actor: AuditActor{
				ID:   r.Header.Get("X-User-ID"),
				Type: "user",
				Role: r.Header.Get("X-User-Role"),
			},
			Resource: AuditResource{
				Type: "api_endpoint",
				ID:   r.URL.Path,
			},
			Action:    r.Method,
			Result:    result,
			IPAddress: r.RemoteAddr,
			UserAgent: r.UserAgent(),
			RequestId: requestID,
			Duration:  time.Since(start),
			Metadata: map[string]interface{}{
				"status_code": wrapped.statusCode,
				"query":       r.URL.RawQuery,
			},
			ComplianceTag: []string{"SOC2", "CC7.1"},
		})
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// SensitiveDataFilter removes PII from logs
type SensitiveDataFilter struct {
	patterns []string
}

func NewSensitiveDataFilter() *SensitiveDataFilter {
	return &SensitiveDataFilter{
		patterns: []string{
			"password", "ssn", "credit_card", "cvv",
			"api_key", "secret", "token", "authorization",
		},
	}
}

func (f *SensitiveDataFilter) Filter(data map[string]interface{}) map[string]interface{} {
	filtered := make(map[string]interface{})
	for k, v := range data {
		if f.isSensitive(k) {
			filtered[k] = "***REDACTED***"
		} else {
			filtered[k] = v
		}
	}
	return filtered
}

func (f *SensitiveDataFilter) isSensitive(key string) bool {
	for _, pattern := range f.patterns {
		if contains([]string{key}, pattern) {
			return true
		}
	}
	return false
}