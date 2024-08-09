package model

import "github.com/pkg/errors"

type Error struct {
	err     error
	message string
}

func (e Error) Error() string {
	return e.message
}

func (e Error) Unwrap() error {
	return e.err
}

func NewError(message string) error {
	return errors.WithStack(Error{
		err:     errors.New(message),
		message: message,
	})
}
