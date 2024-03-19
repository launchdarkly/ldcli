package errors

import "errors"

var (
	ErrInvalidBaseURI = errors.New("baseUri is invalid")
	ErrUnauthorized   = errors.New("You are not authorized to make this request.")
)

type Error struct {
	msg string
}

func New(msg string) Error {
	return Error{
		msg: msg,
	}
}

func (e Error) Error() string {
	return e.msg
}
