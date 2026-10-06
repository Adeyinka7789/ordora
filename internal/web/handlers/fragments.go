package handlers

import (
	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/domain/attachment"
	"github.com/Adeyinka7789/ordora/internal/domain/order"
	"github.com/Adeyinka7789/ordora/internal/domain/payment"
)

// timelineFragment is the data passed to orders/_timeline.html when rendered
// standalone from an HTMX status-change response. When the same fragment is
// included from orders/show.html, the page struct itself is used and its
// fields (Order, CSRFToken, Error) match this shape.
type timelineFragment struct {
	Order     *order.Order
	CSRFToken string
	Error     string
}

// attachmentsFragment is the data passed to orders/_attachments.html when
// rendered standalone from an HTMX upload response.
type attachmentsFragment struct {
	OrderID        uuid.UUID
	Attachments    []*attachment.Attachment
	HasInspiration bool
	CSRFToken      string
}

// hasInspiration reports whether any attachment is a style reference.
func hasInspiration(list []*attachment.Attachment) bool {
	for _, a := range list {
		if a.IsInspiration() {
			return true
		}
	}
	return false
}

// paymentsFragment is passed to orders/_payments.html.
type paymentsFragment struct {
	Order                *order.Order
	Payments             []*payment.Payment
	Balance              int64
	PayStatus            payment.Status
	CSRFToken            string
	Error                string
	AttachmentsByPayment map[uuid.UUID][]*attachment.Attachment
}
