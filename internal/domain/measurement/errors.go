package measurement

import "errors"

var (
	ErrNotFound             = errors.New("measurement: not found")
	ErrTemplateNotFound     = errors.New("measurement: template not found")
	ErrInvalidGender        = errors.New("measurement: gender must be male, female or unisex")
	ErrInvalidGarment       = errors.New("measurement: garment is required")
	ErrInvalidFields        = errors.New("measurement: template needs at least one field")
	ErrFieldKeyRequired     = errors.New("measurement: every field needs a key")
	ErrFieldLabelRequired   = errors.New("measurement: every field needs a label")
	ErrValueRequired        = errors.New("measurement: a required measurement is missing")
	ErrValueTooLong         = errors.New("measurement: measurement value is too long (max 40)")
	ErrNotesTooLong         = errors.New("measurement: notes are too long (max 2000)")
	ErrTemplateNameRequired = errors.New("measurement: template name is required")
	ErrTemplateNotEditable  = errors.New("measurement: system templates cannot be edited, clone it first")
	ErrTooManyFields        = errors.New("measurement: too many fields (max 30)")
	ErrFieldKeyInvalid      = errors.New("measurement: field key must be lowercase letters, numbers or underscore")
)
