package internal

import (
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/pkg/errors"
)

//go:generate go run go.uber.org/mock/mockgen -destination mocks.go -package internal . MockableTime
type MockableTime interface {
	Sleep(duration time.Duration)
	Now() time.Time
}

type realTime struct{}

func (realTime) Sleep(duration time.Duration) {
	time.Sleep(duration)
}

func (realTime) Now() time.Time {
	return time.Now()
}

var timeImpl MockableTime = realTime{}

func Retry429s[T any](requester func() (T, *http.Response, error)) (result T, err error) {
	for {
		var res *http.Response
		result, res, err = requester()
		if res.StatusCode == 429 {
			resetUnixMillisString := res.Header.Get("X-Ratelimit-Reset")
			resetUnixMillis, strconvErr := strconv.ParseInt(resetUnixMillisString, 10, 64)
			if strconvErr != nil {
				err = errors.Wrapf(err, `unable to retry rate limited request: X-RateLimit-Reset: "%s" was not parsable`, resetUnixMillisString)
				return
			}
			sleep := resetUnixMillis - timeImpl.Now().UnixMilli()
			log.Printf("Got 429 in API response. Retrying in %d milliseconds.", sleep)
			timeImpl.Sleep(time.Duration(sleep) * time.Millisecond)
		} else {
			return
		}
	}
}
