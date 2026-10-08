package handlers

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"context"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/domain/attachment"
	"github.com/Adeyinka7789/ordora/internal/domain/order"
	"github.com/Adeyinka7789/ordora/internal/domain/payment"
	"github.com/Adeyinka7789/ordora/internal/domain/product"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
	"github.com/Adeyinka7789/ordora/internal/web/middleware"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// AttachmentHandler serves uploads and downloads.
type AttachmentHandler struct {
	Service       *app.AttachmentService
	PaymentLookup PaymentLookup
	OrderService  OrderLookup         // for re-rendering the fragment
	PaymentSvc    *app.PaymentService // for re-rendering the fragment
	ProductLookup ProductLookup
	Renderer      *render.Renderer
}

// ProductLookup is the minimal interface the attachment handler needs to
// validate a product exists before accepting gallery images.
type ProductLookup interface {
	Get(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) (*product.Product, error)
}

// OrderLookup is the minimal interface the attachment handler needs to
// validate an order exists in the org (upload guard) and reload it for
// fragment re-rendering. *app.OrderService satisfies it; tests stub it.
type OrderLookup interface {
	GetOrder(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) (*order.Order, error)
}

// PaymentLookup is the minimal interface the attachment handler needs to
// resolve a payment id to its order.
type PaymentLookup interface {
	GetByID(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) (*payment.Payment, error)
}

// Upload handles POST /orders/{id}/attachments.
func (h *AttachmentHandler) UploadToOrder(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireWriteScope(w, r)
	if !ok {
		return
	}
	orderID, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}

	// Validate the order exists in this org before storing bytes. Without
	// this, any UUID would create orphaned attachments inside the org.
	if h.OrderService == nil {
		http.Error(w, "order lookup not configured", http.StatusInternalServerError)
		return
	}
	if _, err := h.OrderService.GetOrder(r.Context(), scope, orderID); err != nil {
		http.NotFound(w, r)
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

// UploadToPayment handles POST /payments/{id}/attachments.
//
// After upload, it re-renders the whole payments fragment for the parent
// order, since the receipt needs to appear under the payment row that owns it.
func (h *AttachmentHandler) UploadToPayment(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireWriteScope(w, r)
	if !ok {
		return
	}
	paymentID, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}

	// Resolve the payment to find its order. We need PaymentService here.
	// Attachments handler only knows about attachments; the caller must
	// supply a way to look up the payment.
	if h.PaymentLookup == nil {
		http.Error(w, "payment lookup not configured", http.StatusInternalServerError)
		return
	}
	p, err := h.PaymentLookup.GetByID(r.Context(), scope, paymentID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	_, err = h.readAndStore(r, scope, attachment.EntityPayment, p.ID)
	if err != nil {
		http.Error(w, humanizeAttachmentError(err), http.StatusBadRequest)
		return
	}

	// Re-render the whole payments fragment so the receipt shows under the
	// right payment row.
	h.rendersPaymentsFragmentForOrder(w, r, scope, p.OrderID)
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
	// ?view=1 serves images inline for gallery previews; default stays a
	// download so receipts and files behave as before.
	if r.URL.Query().Get("view") == "1" && strings.HasPrefix(a.MimeType, "image/") {
		w.Header().Set("Content-Disposition", "inline")
		w.Header().Set("Cache-Control", "private, max-age=86400")
	} else {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, a.Filename))
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")

	_, _ = io.Copy(w, body)
}

// UploadToProduct handles POST /products/{id}/images. Only images are
// accepted; the purpose defaults to gallery unless cover was requested.
func (h *AttachmentHandler) UploadToProduct(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireWriteScope(w, r)
	if !ok {
		return
	}
	productID, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	if h.ProductLookup == nil {
		http.Error(w, "product lookup not configured", http.StatusInternalServerError)
		return
	}
	if _, err := h.ProductLookup.Get(r.Context(), scope, productID); err != nil {
		http.NotFound(w, r)
		return
	}

	// The upload form carries purpose=gallery or purpose=cover;
	// readAndStore normalizes anything else to gallery.
	a, err := h.readAndStore(r, scope, attachment.EntityProduct, productID)
	if err != nil {
		http.Error(w, humanizeAttachmentError(err), http.StatusBadRequest)
		return
	}
	if !a.IsImage() {
		_ = h.Service.Delete(r.Context(), scope, a.ID)
		http.Error(w, "Only images (JPG, PNG, WEBP, GIF) are allowed for products.", http.StatusBadRequest)
		return
	}
	// Catalog images are always gallery/cover — never general.
	if !a.IsProductImage() {
		_ = h.Service.SetPurpose(r.Context(), scope, a.ID, attachment.PurposeProductGallery)
	}
	http.Redirect(w, r, "/products/"+productID.String()+"?notice=Image+uploaded.", http.StatusSeeOther)
}

// SetProductCover handles POST /products/{id}/images/{imageID}/cover.
func (h *AttachmentHandler) SetProductCover(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireWriteScope(w, r)
	if !ok {
		return
	}
	productID, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	imageID, ok := parseUUIDParam(w, r, "imageID")
	if !ok {
		return
	}
	if err := h.Service.SetCover(r.Context(), scope, productID, imageID); err != nil {
		http.Error(w, "Could not set cover image.", http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/products/"+productID.String()+"?notice=Cover+image+set.", http.StatusSeeOther)
}

// DeleteProductImage handles POST /products/images/{id}/delete.
func (h *AttachmentHandler) DeleteProductImage(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireWriteScope(w, r)
	if !ok {
		return
	}
	imageID, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	productIDStr := r.URL.Query().Get("product_id")
	productID, err := uuid.Parse(productIDStr)
	if err != nil {
		http.Error(w, "missing product_id", http.StatusBadRequest)
		return
	}
	// Only product images may be deleted here (never order/payment files).
	a, _, err := h.Service.Open(r.Context(), scope, imageID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if a.EntityType != attachment.EntityProduct || a.EntityID != productID {
		http.NotFound(w, r)
		return
	}
	if err := h.Service.Delete(r.Context(), scope, imageID); err != nil {
		http.Error(w, "Could not delete image.", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/products/"+productID.String()+"?notice=Image+deleted.", http.StatusSeeOther)
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
		Purpose:    r.PostFormValue("purpose"),
	})
}

func (h *AttachmentHandler) renderAttachmentsFragment(w http.ResponseWriter, r *http.Request, scope tenant.TenantScope, orderID uuid.UUID) {
	list, err := h.Service.List(r.Context(), scope, attachment.EntityOrder, orderID)
	if err != nil {
		http.Error(w, "could not load attachments", http.StatusInternalServerError)
		return
	}
	h.Renderer.Fragment(w, r, http.StatusOK, "orders/_attachments.html", attachmentsFragment{
		OrderID:        orderID,
		Attachments:    list,
		HasInspiration: hasInspiration(list),
		CSRFToken:      middleware.CSRFTokenFrom(r.Context()),
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

// rendersPaymentsFragmentForOrder loads the order + payments + attachments
// per payment and renders the payments fragment. Used after a payment-level
// attachment upload.
func (h *AttachmentHandler) rendersPaymentsFragmentForOrder(w http.ResponseWriter, r *http.Request, scope tenant.TenantScope, orderID uuid.UUID) {
	o, err := h.OrderService.GetOrder(r.Context(), scope, orderID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	payments, err := h.PaymentSvc.ListForOrder(r.Context(), scope, orderID)
	if err != nil {
		http.Error(w, "could not load payments", http.StatusInternalServerError)
		return
	}

	// Build a per-payment attachments map with one batched query.
	ids := make([]uuid.UUID, 0, len(payments))
	for _, p := range payments {
		ids = append(ids, p.ID)
	}
	attachmentsByPayment := make(map[uuid.UUID][]*attachment.Attachment, len(payments))
	if grouped, err := h.Service.ListForEntities(r.Context(), scope, attachment.EntityPayment, ids); err == nil {
		attachmentsByPayment = grouped
	}

	h.Renderer.Fragment(w, r, http.StatusOK, "orders/_payments.html", paymentsFragment{
		Order:                o,
		Payments:             payments,
		Balance:              o.Balance().Amount(),
		PayStatus:            payment.DeriveStatus(o.Total, o.Paid),
		CSRFToken:            middleware.CSRFTokenFrom(r.Context()),
		AttachmentsByPayment: attachmentsByPayment,
	})
}
