package handlers

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/domain/attachment"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
	"github.com/Adeyinka7789/ordora/internal/web/middleware"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// AttachmentHandler serves uploads and downloads.
type AttachmentHandler struct {
	Service  *app.AttachmentService
	Renderer *render.Renderer
}

// Upload handles POST /orders/{id}/attachments.
func (h *AttachmentHandler) UploadToOrder(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	orderID, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}

	_, err := h.readAndStore(r, scope, attachment.EntityOrder, orderID)
	if err != nil {
		http.Error(w, humanizeAttachmentError(err), http.StatusBadRequest)
		return
	}

	if isHTMX(r) {
		h.renderAttachmentsFragment(w, r, scope, orderID)
		return
	}
	http.Redirect(w, r, "/orders/"+orderID.String()+"?notice=Attachment+uploaded.", http.StatusSeeOther)
}

// Download handles GET /attachments/{id}.
func (h *AttachmentHandler) Download(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}

	a, body, err := h.Service.Open(r.Context(), scope, id)
	if err != nil {
		if errors.Is(err, attachment.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "could not open attachment", http.StatusInternalServerError)
		return
	}
	defer body.Close()

	w.Header().Set("Content-Type", a.MimeType)
	w.Header().Set("Content-Length", strconv.FormatInt(a.SizeBytes, 10))
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, a.Filename))
	w.Header().Set("X-Content-Type-Options", "nosniff")

	_, _ = io.Copy(w, body)
}

// readAndStore parses a multipart upload and hands it to the service.
//
// The file is buffered fully in memory before storage. This is acceptable
// because attachments are capped at 10 MiB. If the cap is ever raised, switch
// to a temp-file streaming approach.
func (h *AttachmentHandler) readAndStore(r *http.Request, scope tenant.TenantScope, entityType attachment.EntityType, entityID uuid.UUID) (*attachment.Attachment, error) {
	r.Body = http.MaxBytesReader(nil, r.Body, attachment.MaxSizeBytes+1024*1024)

	if err := r.ParseMultipartForm(attachment.MaxSizeBytes + 1024*1024); err != nil {
		return nil, attachment.ErrSizeTooLarge
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		return nil, attachment.ErrFilenameRequired
	}
	defer file.Close()

	// Sniff MIME from the first 512 bytes.
	head := make([]byte, 512)
	n, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, err
	}
	head = head[:n]
	sniffed := http.DetectContentType(head)

	// Reconstruct the full stream: head + rest of file.
	full, err := io.ReadAll(io.MultiReader(bytes.NewReader(head), file))
	if err != nil {
		return nil, err
	}

	return h.Service.Upload(r.Context(), scope, app.UploadInput{
		EntityType: entityType,
		EntityID:   entityID,
		Filename:   header.Filename,
		MimeType:   sniffed,
		Size:       int64(len(full)),
		Body:       bytes.NewReader(full),
	})
}

func (h *AttachmentHandler) renderAttachmentsFragment(w http.ResponseWriter, r *http.Request, scope tenant.TenantScope, orderID uuid.UUID) {
	list, err := h.Service.List(r.Context(), scope, attachment.EntityOrder, orderID)
	if err != nil {
		http.Error(w, "could not load attachments", http.StatusInternalServerError)
		return
	}
	h.Renderer.Fragment(w, r, http.StatusOK, "orders/_attachments.html", attachmentsFragment{
		OrderID:     orderID,
		Attachments: list,
		CSRFToken:   middleware.CSRFTokenFrom(r.Context()),
	})
}

func humanizeAttachmentError(err error) string {
	switch {
	case errors.Is(err, attachment.ErrMimeNotAllowed):
		return "That file type is not allowed. Allowed: JPG, PNG, WEBP, GIF, PDF."
	case errors.Is(err, attachment.ErrSizeTooLarge):
		return "File is too large. Maximum size is 10 MiB."
	case errors.Is(err, attachment.ErrSizeZero):
		return "The file is empty."
	case errors.Is(err, attachment.ErrFilenameRequired):
		return "Please choose a file."
	default:
		return "Could not upload file: " + err.Error()
	}
}
