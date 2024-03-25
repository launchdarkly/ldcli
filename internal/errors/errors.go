package errors

import (
	"github.com/pkg/errors"

	ldapi "github.com/launchdarkly/api-client-go/v14"
)

var ErrInvalidBaseURI = NewError("baseUri is invalid")

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

func (e Error) Is(err error) bool {
	_, ok := err.(Error)

	return ok
}

func NewError(message string) error {
	return errors.WithStack(Error{
		err:     errors.New(message),
		message: message,
	})
}

func NewErrorWrapped(message string, underlying error) error {
	return errors.WithStack(Error{
		err:     underlying,
		message: message,
	})
}

func NewApiError(err error) ([]byte, error) {
	var ldErr *ldapi.GenericOpenAPIError
	ok := errors.As(err, &ldErr)
	if ok {
		return nil, NewErrorWrapped(string(ldErr.Body()), ldErr)
	}

	return nil, err
}
