package retention

import "errors"

var (
	// ErrInvalidDays reports a retention window that is negative or beyond maxDays.
	ErrInvalidDays = errors.New("retention: invalid number of days")

	// ErrRunInProgress reports that another sweep already holds the lock. It is a
	// conflict rather than a failure: the work is being done, just not by this
	// caller.
	ErrRunInProgress = errors.New("retention: a sweep is already running")
)
