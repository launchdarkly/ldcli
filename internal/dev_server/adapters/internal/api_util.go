package internal

import (
	"context"
	"log"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/launchdarkly/api-client-go/v14"
	"github.com/pkg/errors"
)

// maxConcurrentPageFetches bounds how many pages we fetch at once once the total
// item count is known. Kept modest to stay well clear of the API's rate limiter;
// Retry429s still handles any 429s that slip through.
const maxConcurrentPageFetches = 6

// GetPaginatedItems fetches all pages of a paginated list endpoint. It fetches
// page 0 first to learn the total item count, then fetches the remaining pages
// concurrently (bounded by maxConcurrentPageFetches) instead of following the
// `next` link serially, since every offset is already known once the total
// count and page size are known.
func GetPaginatedItems[T any, R interface {
	GetItems() []T
	GetLinks() map[string]ldapi.Link
	GetTotalCount() int32
}](ctx context.Context, projectKey string, fetchFunc func(context.Context, string, *int64, *int64) (R, error)) ([]T, error) {
	first, err := fetchFunc(ctx, projectKey, nil, nil)
	if err != nil {
		return nil, err
	}

	firstItems := first.GetItems()
	limit := int64(len(firstItems))
	total := int64(first.GetTotalCount())
	if limit == 0 || total <= limit {
		return firstItems, nil
	}

	numPages := int((total + limit - 1) / limit)
	pages := make([][]T, numPages)
	pages[0] = firstItems

	sem := make(chan struct{}, maxConcurrentPageFetches)
	var wg sync.WaitGroup
	errs := make([]error, numPages)

	for page := 1; page < numPages; page++ {
		page := page
		offset := int64(page) * limit
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			result, fetchErr := fetchFunc(ctx, projectKey, &limit, &offset)
			if fetchErr != nil {
				errs[page] = fetchErr
				return
			}
			pages[page] = result.GetItems()
		}()
	}
	wg.Wait()

	for _, pageErr := range errs {
		if pageErr != nil {
			return nil, pageErr
		}
	}

	items := make([]T, 0, total)
	for _, p := range pages {
		items = append(items, p...)
	}
	return items, nil
}

//go:generate go run go.uber.org/mock/mockgen -destination mocks.go -package internal . MockableTime
type MockableTime interface {
	Sleep(duration time.Duration)
	Now() time.Time
	// Jitter returns a random duration in [0, max). Used to decorrelate
	// retries after a 429: with concurrent page fetches, several requests
	// can get rate-limited at once and would otherwise all compute the same
	// X-Ratelimit-Reset and wake up to retry at the exact same instant,
	// recreating the burst that got them limited in the first place.
	Jitter(max time.Duration) time.Duration
}

type realTime struct{}

func (realTime) Sleep(duration time.Duration) {
	time.Sleep(duration)
}

func (realTime) Now() time.Time {
	return time.Now()
}

func (realTime) Jitter(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	return rand.N(max)
}

var timeImpl MockableTime = realTime{}

// maxRetryJitter bounds the random extra delay added on top of the API's
// requested X-Ratelimit-Reset wait, so concurrent retries spread out instead
// of waking up in lockstep.
const maxRetryJitter = 250 * time.Millisecond

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
			sleep := time.Duration(resetUnixMillis-timeImpl.Now().UnixMilli())*time.Millisecond + timeImpl.Jitter(maxRetryJitter)
			log.Printf("Got 429 in API response. Retrying in %s.", sleep)
			timeImpl.Sleep(sleep)
		} else {
			return
		}
	}
}
