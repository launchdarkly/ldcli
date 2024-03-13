package errors

import "errors"

var (
	ErrForbidden      = errors.New("You do not have permission to make this request")
	ErrInvalidBaseURI = errors.New("baseUri is invalid")
	ErrUnauthorized   = errors.New("You are not authorized to make this request")
)
