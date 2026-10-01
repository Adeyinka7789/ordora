package product

import "errors"

var (
	ErrNameRequired       = errors.New("product: name is required")
	ErrNameTooLong        = errors.New("product: name is too long (max 200)")
	ErrDescriptionTooLong = errors.New("product: description is too long (max 5000)")
	ErrSKUTooLong         = errors.New("product: SKU is too long (max 64)")
	ErrPriceNegative      = errors.New("product: price cannot be negative")
	ErrCurrencyRequired   = errors.New("product: currency is required")
	ErrCurrencyMismatch   = errors.New("product: currency mismatch")
	ErrNotFound           = errors.New("product: not found")
)
