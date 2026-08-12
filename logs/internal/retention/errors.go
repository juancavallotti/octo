package retention

import "errors"

// ErrInvalidDays reports a retention window that is negative or beyond maxDays.
var ErrInvalidDays = errors.New("retention: invalid number of days")
