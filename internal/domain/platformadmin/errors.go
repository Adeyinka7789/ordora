package platformadmin

import "errors"

var (
	ErrEmailRequired      = errors.New("platformadmin: email is required")
	ErrEmailInvalid       = errors.New("platformadmin: email is invalid")
	ErrNameRequired       = errors.New("platformadmin: name is required")
	ErrPasswordRequired   = errors.New("platformadmin: password is required")
	ErrNotFound           = errors.New("platformadmin: not found")
	ErrDisabled           = errors.New("platformadmin: account disabled")
	ErrInvalidCredentials = errors.New("platformadmin: invalid credentials")
)
