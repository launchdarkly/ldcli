package internal

import (
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/pkg/errors"
)

// FetchPagesConcurrently returns every item across an offset-paginated list, fetching pages in bounded concurrent batches until a short page ends the list. It never trusts a reported total count (0 when absent would truncate); a short page is the only end signal.
func FetchPagesConcurrently[T any](pageSize, concurrency int, fetch func(offset int64) ([]T, error)) ([]T, error) {
	first, err := fetch(0)
	if err != nil {
		return nil, err
	}
	all := first
	if len(first) < pageSize {
		return all, nil
	}

	for offset := int64(pageSize); ; offset += int64(concurrency) * int64(pageSize) {
		batch := make([][]T, concurrency)
		errs := make([]error, concurrency)
		var wg sync.WaitGroup
		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				batch[i], errs[i] = fetch(offset + int64(i)*int64(pageSize))
			}(i)
		}
		wg.Wait()

		// Stop at the first short/empty page; pages after it are speculative probes past the end, so ignore their errors.
		for i := 0; i < concurrency; i++ {
			if errs[i] != nil {
				return nil, errs[i]
			}
			all = append(all, batch[i]...)
			if len(batch[i]) < pageSize {
				return all, nil
			}
		}
	}
}

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
		// Only bail on a nil response (transport failure); a 429 comes back as a non-nil error with a non-nil response, so never bail on err alone or the retry is skipped.
		if res == nil {
			return
		}
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
