package errors

import (
	"encoding/json"
	"fmt"

	"github.com/pkg/errors"

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

// LDAPIError is an error from the LaunchDarkly API client.
type LDAPIError interface {
	Body() []byte
	Error() string
	Model() interface{}
}

type APIError struct {
	body  []byte
	err   error
	model interface{}
}

func NewAPIError(body []byte, err error, model interface{}) APIError {
	return APIError{
		body:  body,
		err:   err,
		model: model,
	}
}

func (e APIError) Error() string {
	return e.err.Error()
}

func (e APIError) Body() []byte {
	return e.body
}

func (e APIError) Model() interface{} {
	return e.model
}

// NewLDAPIError converts the error returned from API calls to LaunchDarkly to have a
// consistent Error() JSON structure.
func NewLDAPIError(err error) error {
	var apiErr LDAPIError
	ok := errors.As(err, &apiErr)
	if ok {
		// the 401 response does not have a body, so we need to create one for a
		// consistent response
		if err.Error() == "401 Unauthorized" {
			errMsg, err := normalizeUnauthorizedJSON()
			if err != nil {
				return err
			}

			return NewError(string(errMsg))
		}

		// otherwise return the error's body as the message
		return NewErrorWrapped(string(apiErr.Body()), apiErr)
	}

	return err
}

func normalizeUnauthorizedJSON() ([]byte, error) {
	e := struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{
		Code:    "unauthorized",
		Message: "You do not have access to perform this action",
	}
	errMsg, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}

	return errMsg, nil
}
