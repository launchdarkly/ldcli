package errors_test

import (
	"encoding/json"
	"errors"
	"testing"

	ldapi "github.com/launchdarkly/api-client-go/v14"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	errs "ldcli/internal/errors"
)

func TestAPIError(t *testing.T) {
	t.Run("with a 400 error has a JSON response", func(t *testing.T) {
		underlying := errs.NewAPIError(
			[]byte(`{"code":"conflict","message":"an error"}`),
			errors.New("400 an error"),
			[]string{},
		)

		err := errs.NewLDAPIError(underlying)

		require.Error(t, err)
		assert.JSONEq(t, `{"code": "conflict", "message": "an error"}`, err.Error())
	})

	t.Run("with a 401 error has a JSON response", func(t *testing.T) {
		rep := ldapi.UnauthorizedErrorRep{}
		repBytes, err := json.Marshal(rep)
		require.NoError(t, err)
		underlying := errs.NewAPIError(
			repBytes,
			errors.New("401 Unauthorized"),
			[]string{},
		)

		err = errs.NewLDAPIError(underlying)

		require.Error(t, err)
		assert.JSONEq(t, `{"code": "unauthorized", "message": "You do not have access to perform this action"}`, err.Error())
	})

	t.Run("with a 403 error has a JSON response", func(t *testing.T) {
		underlying := errs.NewAPIError(
			[]byte(`{"code": "forbidden", "message": "an error"}`),
			errors.New("403 Forbidden"),
			[]string{},
		)

		err := errs.NewLDAPIError(underlying)

		require.Error(t, err)
		assert.JSONEq(t, `{"code": "forbidden", "message": "an error"}`, err.Error())
	})
}
