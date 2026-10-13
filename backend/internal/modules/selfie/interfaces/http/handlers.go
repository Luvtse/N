package http

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	selfieapp "nidaw-backend/internal/modules/selfie/application"
	selfiedomain "nidaw-backend/internal/modules/selfie/domain"
	"nidaw-backend/internal/shared/auth"
)

// MaxUploadBytes is the hard ceiling enforced at the HTTP layer before the
// request body is even read into memory. The service re-validates; this is
// defense in depth so a malicious client cannot stream 10 GB against us.
const MaxUploadBytes = 200 * 1024 // 200 KB, per plan §2.3

// Handlers exposes the pre-selfie REST surface:
//
//	POST /driver/rides/{rideId}/pre-selfie   (driver app upload, multipart)
//	GET  /selfies/{id}?exp={unix}&sig={hex}  (participant redemption of signed URL)
type Handlers struct {
	svc *selfieapp.Service
	log *zap.Logger
}

func NewHandlers(svc *selfieapp.Service, log *zap.Logger) *Handlers {
	return &Handlers{svc: svc, log: log}
}

// Register mounts the routes on the given chi router. Callers mount under
// AuthRequired + ExtractUserToContext middleware; identity is read from the
// JWT subject only — never from the request body. Both handlers require an
// authenticated caller: upload because the driver's identity must come from
// the token, and redemption because the participant gate inside Redeem needs
// a trustworthy viewer id (the HMAC signature proves link validity, not who
// is holding it).
func (h *Handlers) Register(r chi.Router) {
	r.Post("/driver/rides/{rideId}/pre-selfie", h.upload)
	r.Get("/selfies/{id}", h.redeem)
}

// upload handles the driver's pre-selfie capture upload.
func (h *Handlers) upload(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserIDFromContext(r.Context())
	if !ok || userID == uuid.Nil {
		writeErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rideID, err := uuid.Parse(chi.URLParam(r, "rideId"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid ride id")
		return
	}

	// Cap the whole request body; multipart overhead is small for a ≤200 KB image.
	r.Body = http.MaxBytesReader(w, r.Body, MaxUploadBytes+64*1024)
	if err := r.ParseMultipartForm(MaxUploadBytes); err != nil {
		if isMediaTypeErr(err) {
			writeErr(w, http.StatusBadRequest, "multipart form with an 'image' file part is required")
			return
		}
		writeErr(w, http.StatusRequestEntityTooLarge, "upload exceeds the 200 KB limit")
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			r.MultipartForm.RemoveAll()
		}
	}()

	files := r.MultipartForm.File["image"]
	if len(files) == 0 {
		writeErr(w, http.StatusBadRequest, "missing 'image' file part")
		return
	}
	header := files[0]
	file, err := header.Open()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to open upload")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, MaxUploadBytes+1))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to read upload")
		return
	}

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}

	selfie, err := h.svc.Upload(selfieapp.UploadInput{
		RideID:      rideID,
		DriverID:    userID,
		ContentType: contentType,
		Data:        data,
	})
	if err != nil {
		h.mapUploadErr(w, err)
		return
	}

	h.log.Info("pre-selfie uploaded",
		zap.String("ride_id", rideID.String()),
		zap.String("driver_id", userID.String()),
		zap.String("selfie_id", selfie.ID.String()),
	)

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":          selfie.ID.String(),
		"captured_at": selfie.CapturedAt.UTC().Format(time.RFC3339),
		"expires_at":  selfie.ExpiresAt.UTC().Format(time.RFC3339),
		"size_bytes":  len(data),
	})
}

func (h *Handlers) mapUploadErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, selfiedomain.ErrForbidden):
		writeErr(w, http.StatusForbidden, err.Error())
	case errors.Is(err, selfiedomain.ErrInvalidRideID):
		writeErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, selfiedomain.ErrImageTooLarge):
		writeErr(w, http.StatusRequestEntityTooLarge, err.Error())
	case errors.Is(err, selfiedomain.ErrEmptyImage),
		errors.Is(err, selfiedomain.ErrBadContentType):
		writeErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, selfiedomain.ErrStorageFailed):
		// Honest degradation: storage down is a real 503, never a fake success.
		writeErr(w, http.StatusServiceUnavailable, "selfie storage unavailable — retry shortly")
	default:
		h.log.Error("pre-selfie upload failed", zap.Error(err))
		writeErr(w, http.StatusInternalServerError, "internal error")
	}
}

// redeem serves the selfie bytes to a ride participant presenting a valid,
// unexpired signature. Fail-closed on any verification failure.
func (h *Handlers) redeem(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserIDFromContext(r.Context())
	if !ok || userID == uuid.Nil {
		writeErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid selfie id")
		return
	}
	expRaw := r.URL.Query().Get("exp")
	sig := r.URL.Query().Get("sig")
	expUnix, err := strconv.ParseInt(expRaw, 10, 64)
	if err != nil || sig == "" {
		writeErr(w, http.StatusBadRequest, "exp and sig query parameters are required")
		return
	}

	res, err := h.svc.Redeem(id, time.Unix(expUnix, 0).UTC(), userID, sig)
	if err != nil {
		switch {
		case errors.Is(err, selfiedomain.ErrNotFound):
			writeErr(w, http.StatusNotFound, "selfie not found")
		case errors.Is(err, selfiedomain.ErrForbidden):
			writeErr(w, http.StatusForbidden, "only ride participants can view this selfie")
		case errors.Is(err, selfieapp.ErrBadSignature):
			writeErr(w, http.StatusForbidden, "invalid signature")
		case errors.Is(err, selfieapp.ErrExpired):
			writeErr(w, http.StatusGone, "this selfie link has expired")
		default:
			h.log.Error("selfie redemption failed", zap.Error(err))
			writeErr(w, http.StatusInternalServerError, "internal error")
		}
		return
	}

	w.Header().Set("Content-Type", res.Selfie.ContentType)
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(res.Data); err != nil {
		h.log.Warn("selfie write failed", zap.Error(err))
	}
}

func isMediaTypeErr(err error) bool {
	return err != nil && (errors.Is(err, http.ErrNotMultipart) || errors.Is(err, ErrNoMultipartBoundary))
}

// ErrNoMultipartBoundary distinguishes malformed multipart bodies (400) from
// oversize uploads (413).
var ErrNoMultipartBoundary = errors.New("malformed multipart body")

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
