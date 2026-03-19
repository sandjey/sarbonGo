package calls

import "errors"

var ErrNotFound = errors.New("call not found")
var ErrForbidden = errors.New("forbidden")
var ErrInvalidState = errors.New("invalid state transition")
var ErrAlreadyEnded = errors.New("call already ended")

