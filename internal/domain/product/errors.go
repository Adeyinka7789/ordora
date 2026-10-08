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

	ErrMaterialTooLong     = errors.New("product: material is too long (max 120)")
	ErrColorTooLong        = errors.New("product: color is too long (max 120)")
	ErrShortDescTooLong    = errors.New("product: short description is too long (max 500)")
	ErrNotesTooLong        = errors.New("product: internal notes are too long (max 5000)")
	ErrSpecsTooLong        = errors.New("product: specifications are too long (max 2000)")
	ErrProductionDays      = errors.New("product: estimated production days cannot be negative")
	ErrAvailabilityInvalid = errors.New("product: invalid availability")
	ErrCategoryTooLong     = errors.New("product: category is too long (max 80)")
	ErrQuestionLabel       = errors.New("product: each question needs a label (max 120)")
	ErrQuestionKey         = errors.New("product: invalid question key")
	ErrTooManyQuestions    = errors.New("product: too many questions (max 10)")
)
