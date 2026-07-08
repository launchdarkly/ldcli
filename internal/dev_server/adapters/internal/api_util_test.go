package internal

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestRetry429s(t *testing.T) {
	t.Run("it should call exactly once if not a 429", func(t *testing.T) {
		called := 0
		res, err := Retry429s(func() (string, *http.Response, error) {
			called++
			return "lol", &http.Response{StatusCode: 200}, nil
		})
		assert.Equal(t, "lol", res)
		assert.NoError(t, err)
		assert.Equal(t, 1, called)
	})

	t.Run("it should retry when a 429 is received", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		timeMock := NewMockMockableTime(ctrl)
		defer func() { ctrl.Finish() }()
		timeImpl = timeMock
		defer func() { timeImpl = realTime{} }()
		timeMock.EXPECT().Now().Return(time.UnixMilli(0))
		timeMock.EXPECT().Sleep(time.Duration(1000) * time.Millisecond)

		called := 0
		res, err := Retry429s(func() (string, *http.Response, error) {
			called++
			if called > 1 {
				return "lol", &http.Response{StatusCode: 200}, nil
			} else {
				header := make(http.Header)
				header.Set("X-Ratelimit-Reset", "1000")
				return "", &http.Response{StatusCode: 429, Header: header}, nil
			}
		})
		assert.Equal(t, "lol", res)
		assert.NoError(t, err)
		assert.Equal(t, 2, called)
	})
}
