// Package domain holds the platform's entities and business rules. It depends
// on nothing outside the standard library: no HTTP, no SQL.
package domain

import (
	"errors"
	"math"
)

// Error kinds; adapters translate them into transport status codes.
var (
	ErrInvalid     = errors.New("invalid")
	ErrNotFound    = errors.New("not found")
	ErrConflict    = errors.New("conflict")
	ErrUnavailable = errors.New("unavailable")
)

// Error is a failure with a message meant for the user; Cause keeps the
// underlying error for logs.
type Error struct {
	Kind  error
	Msg   string
	Cause error
}

func (e *Error) Error() string   { return e.Msg }
func (e *Error) Unwrap() []error { return []error{e.Kind, e.Cause} }

func Invalid(msg string) error  { return &Error{Kind: ErrInvalid, Msg: msg} }
func NotFound(msg string) error { return &Error{Kind: ErrNotFound, Msg: msg} }
func Conflict(msg string) error { return &Error{Kind: ErrConflict, Msg: msg} }

// Unavailable reports that a dependency (usually the database) failed.
func Unavailable(msg string, cause error) error {
	return &Error{Kind: ErrUnavailable, Msg: msg, Cause: cause}
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// Round2 rounds to two decimals, the precision the web shows.
func Round2(v float64) float64 { return math.Round(v*100) / 100 }
