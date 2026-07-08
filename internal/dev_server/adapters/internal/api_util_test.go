package internal

import (
	"context"
	"net/http"
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
