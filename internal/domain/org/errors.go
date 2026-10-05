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

	ErrProfileCategoryRequired = errors.New("org: business category is required")
	ErrProfileCategoryInvalid  = errors.New("org: business category is invalid")
	ErrProfileTypeInvalid      = errors.New("org: business type is invalid")
	ErrProfileTeamSizeInvalid  = errors.New("org: team size is invalid")
	ErrProfileReferralInvalid  = errors.New("org: referral source is invalid")
	ErrProfilePhoneInvalid     = errors.New("org: business phone is invalid")

	ErrNotFound = errors.New("org: not found")
)
