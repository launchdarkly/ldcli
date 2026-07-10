package internal

import (
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/pkg/errors"
)

// FetchPagesConcurrently returns every item across an offset-paginated list.
//
// It fetches page 0, and only if that page is full keeps pulling pages in
// bounded concurrent batches (concurrency at a time) until a short page marks
// the end of the list. It deliberately never relies on a reported total count:
// that field is optional and reads back as 0 when absent, which would silently
// truncate a large result set. A short (or empty) page is the only end signal.
// Small lists cost a single request; large ones parallelise instead of paging
// serially. The first page error is returned as-is.
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

		// Pages are contiguous by offset, so the first short/empty page ends the
		// list and every page after it in the batch is empty.
		done := false
		for i := 0; i < concurrency; i++ {
			if errs[i] != nil {
				return nil, errs[i]
			}
			all = append(all, batch[i]...)
			if len(batch[i]) < pageSize {
				done = true
			}
		}
		if done {
			return all, nil
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
		// On a transport-level failure (DNS, connection refused, timeout) the
		// client returns a nil response, so guard before touching it - otherwise
		// the deref panics. A 429 arrives as a non-nil error *with* a non-nil
		// response, so only bail on a nil response, never on err alone, or we'd
		// skip the rate-limit retry below.
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
