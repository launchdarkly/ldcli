package internal

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	ldapi "github.com/launchdarkly/api-client-go/v14"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

type testItem struct {
	ID string
}

type testResult struct {
	items      []testItem
	links      map[string]ldapi.Link
	totalCount int32
}

func (r testResult) GetItems() []testItem {
	return r.items
}

func (r testResult) GetLinks() map[string]ldapi.Link {
	return r.links
}

func (r testResult) GetTotalCount() int32 {
	return r.totalCount
}

func TestGetPaginatedItems(t *testing.T) {
	ctx := context.Background()
	projectKey := "test-project"

	t.Run("single page", func(t *testing.T) {
		fetchFunc := func(ctx context.Context, projectKey string, limit, offset *int64) (testResult, error) {
			assert.Nil(t, limit)
			assert.Nil(t, offset)
			return testResult{items: []testItem{{ID: "1"}, {ID: "2"}}, totalCount: 2}, nil
		}

		items, err := GetPaginatedItems(ctx, projectKey, fetchFunc)
		assert.NoError(t, err)
		assert.Equal(t, []testItem{{ID: "1"}, {ID: "2"}}, items)
	})

	t.Run("multiple pages fetched by offset, order preserved regardless of arrival order", func(t *testing.T) {
		pages := map[int64][]testItem{
			0: {{ID: "1"}, {ID: "2"}},
			2: {{ID: "3"}, {ID: "4"}},
			4: {{ID: "5"}},
		}

		fetchFunc := func(ctx context.Context, projectKey string, limit, offset *int64) (testResult, error) {
			if offset == nil {
				return testResult{items: pages[0], totalCount: 5}, nil
			}
			return testResult{items: pages[*offset]}, nil
		}

		items, err := GetPaginatedItems(ctx, projectKey, fetchFunc)
		assert.NoError(t, err)
		assert.Equal(t, []testItem{{ID: "1"}, {ID: "2"}, {ID: "3"}, {ID: "4"}, {ID: "5"}}, items)
	})

	t.Run("error on any page propagates", func(t *testing.T) {
		fetchFunc := func(ctx context.Context, projectKey string, limit, offset *int64) (testResult, error) {
			if offset == nil {
				return testResult{items: []testItem{{ID: "1"}, {ID: "2"}}, totalCount: 4}, nil
			}
			return testResult{}, assert.AnError
		}

		_, err := GetPaginatedItems(ctx, projectKey, fetchFunc)
		assert.Error(t, err)
	})

	t.Run("empty response", func(t *testing.T) {
		fetchFunc := func(ctx context.Context, projectKey string, limit, offset *int64) (testResult, error) {
			return testResult{items: []testItem{}, totalCount: 0}, nil
		}

		items, err := GetPaginatedItems(ctx, projectKey, fetchFunc)
		assert.NoError(t, err)
		assert.Equal(t, []testItem{}, items)
	})

	t.Run("remaining pages are fetched concurrently, not one at a time", func(t *testing.T) {
		var mu sync.Mutex
		inFlight := 0
		maxInFlight := 0

		fetchFunc := func(ctx context.Context, projectKey string, limit, offset *int64) (testResult, error) {
			if offset == nil {
				return testResult{items: []testItem{{ID: "0"}}, totalCount: 4}, nil
			}
			mu.Lock()
			inFlight++
			if inFlight > maxInFlight {
				maxInFlight = inFlight
			}
			mu.Unlock()

			time.Sleep(20 * time.Millisecond)

			mu.Lock()
			inFlight--
			mu.Unlock()
			return testResult{items: []testItem{{ID: "x"}}}, nil
		}

		_, err := GetPaginatedItems(ctx, projectKey, fetchFunc)
		assert.NoError(t, err)
		assert.Greater(t, maxInFlight, 1, "expected more than one page in flight at once")
	})
}

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

	t.Run("it should retry when a 429 is received, adding jitter on top of the reset wait", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		timeMock := NewMockMockableTime(ctrl)
		defer func() { ctrl.Finish() }()
		timeImpl = timeMock
		defer func() { timeImpl = realTime{} }()
		timeMock.EXPECT().Now().Return(time.UnixMilli(0))
		timeMock.EXPECT().Jitter(maxRetryJitter).Return(50 * time.Millisecond)
		timeMock.EXPECT().Sleep(1050 * time.Millisecond)

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

	t.Run("concurrent retries get decorrelated jitter instead of waking up in lockstep", func(t *testing.T) {
		// Simulates several concurrent page fetches all getting rate-limited
		// with the same X-Ratelimit-Reset at once. With realTime's actual
		// Jitter (not mocked here), their computed sleep durations should
		// differ instead of being identical, so they don't all retry at the
		// exact same instant and recreate the burst that got them limited.
		reset := time.Now().Add(20 * time.Millisecond).UnixMilli()
		resetStr := strconv.FormatInt(reset, 10)

		const n = 8
		sleeps := make([]time.Duration, n)
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			i := i
			wg.Add(1)
			go func() {
				defer wg.Done()
				called := 0
				start := time.Now()
				_, err := Retry429s(func() (string, *http.Response, error) {
					called++
					if called > 1 {
						return "ok", &http.Response{StatusCode: 200}, nil
					}
					header := make(http.Header)
					header.Set("X-Ratelimit-Reset", resetStr)
					return "", &http.Response{StatusCode: 429, Header: header}, nil
				})
				assert.NoError(t, err)
				sleeps[i] = time.Since(start)
			}()
		}
		wg.Wait()

		distinct := map[time.Duration]bool{}
		for _, s := range sleeps {
			distinct[s] = true
		}
		assert.Greater(t, len(distinct), 1, "expected jittered sleep durations to differ across concurrent retries")
	})
}
