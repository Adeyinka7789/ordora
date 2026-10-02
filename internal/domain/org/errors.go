package org

import "errors"

var (
	ErrNameRequired = errors.New("org: name is required")
	ErrNameInvalid  = errors.New("org: name is invalid")

	ErrSlugRequired = errors.New("org: slug is required")
	ErrSlugLength   = errors.New("org: slug must be 3–60 characters")
	ErrSlugInvalid  = errors.New("org: slug contains invalid characters")
	ErrSlugTaken    = errors.New("org: slug already taken")

	ErrCurrencyInvalid = errors.New("org: currency must be a 3-letter ISO code")

	ErrNotFound = errors.New("org: not found")
)
