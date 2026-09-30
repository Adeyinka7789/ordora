package user

import "errors"

var (
	ErrEmailRequired        = errors.New("user: email is required")
	ErrEmailInvalid         = errors.New("user: email is invalid")
	ErrEmailTooLong         = errors.New("user: email is too long")
	ErrNameRequired         = errors.New("user: name is required")
	ErrPasswordHashRequired = errors.New("user: password hash is required")
	ErrNotFound             = errors.New("user: not found")
	ErrEmailTaken           = errors.New("user: email already registered")
)
