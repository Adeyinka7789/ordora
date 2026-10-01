package order

import "errors"

var (
	ErrNotFound           = errors.New("order: not found")
	ErrTitleRequired      = errors.New("order: title is required")
	ErrTitleTooLong       = errors.New("order: title is too long (max 200)")
	ErrDescriptionTooLong = errors.New("order: description is too long (max 5000)")
	ErrNoItems            = errors.New("order: an order must have at least one item")
	ErrItemDescription    = errors.New("order: item description is required")
	ErrItemQuantity       = errors.New("order: item quantity must be positive")
	ErrItemPriceNegative  = errors.New("order: item unit price cannot be negative")
	ErrCurrencyMismatch   = errors.New("order: currency mismatch between order and item")
	ErrDiscountNegative   = errors.New("order: discount cannot be negative")
	ErrTaxNegative        = errors.New("order: tax cannot be negative")
	ErrDiscountTooLarge   = errors.New("order: discount cannot exceed subtotal")
	ErrAlreadyCancelled   = errors.New("order: order is already cancelled")
	ErrAlreadyCompleted   = errors.New("order: order is already completed")
)
