package errors

import "errors"

var (
	ErrInvalidBaseURI = errors.New("baseUri is invalid")
	ErrUnauthorized   = errors.New("You are not authorized to make this request.")
)
