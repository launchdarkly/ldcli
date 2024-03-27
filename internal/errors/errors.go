package errors

import (
	"fmt"

	"github.com/pkg/errors"

	ldapi "github.com/launchdarkly/api-client-go/v14"

	"ldcli/cmd/cliflags"
)

var ErrInvalidBaseURI = NewError(fmt.Sprintf("%s is invalid", cliflags.BaseURIFlag))

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

func NewAPIError(err error) error {
	var ldErr *ldapi.GenericOpenAPIError
	ok := errors.As(err, &ldErr)
	if ok {
		return NewErrorWrapped(string(ldErr.Body()), ldErr)
	}

	return err
}
