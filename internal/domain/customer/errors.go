package customer

import "errors"

var (
	ErrNameRequired   = errors.New("customer: name is required")
	ErrNameTooLong    = errors.New("customer: name is too long (max 200)")
	ErrEmailInvalid   = errors.New("customer: email is invalid")
	ErrEmailTooLong   = errors.New("customer: email is too long")
	ErrPhoneTooLong   = errors.New("customer: phone is too long (max 40)")
	ErrAddressTooLong = errors.New("customer: address is too long (max 500)")
	ErrNotesTooLong   = errors.New("customer: notes are too long (max 5000)")
	ErrNotFound       = errors.New("customer: not found")
)
