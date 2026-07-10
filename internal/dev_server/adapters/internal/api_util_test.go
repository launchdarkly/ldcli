package internal

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestFetchPagesConcurrently(t *testing.T) {
	const pageSize, concurrency = 100, 6

	// pager returns a fetch func that serves `total` sequential ints across
	// pages of pageSize, recording every offset requested.
	pager := func(total int) (func(offset int64) ([]int, error), *[]int64) {
		var requested []int64
		fetch := func(offset int64) ([]int, error) {
			requested = append(requested, offset)
			var page []int
			for i := offset; i < offset+pageSize && i < int64(total); i++ {
				page = append(page, int(i))
			}
			return page, nil
		}
		return fetch, &requested
	}

	t.Run("single short page costs one request", func(t *testing.T) {
		fetch, requested := pager(42)
		items, err := FetchPagesConcurrently(pageSize, concurrency, fetch)
		require.NoError(t, err)
		assert.Len(t, items, 42)
		assert.Equal(t, []int64{0}, *requested)
	})

	t.Run("exactly one full page still probes for more, then stops", func(t *testing.T) {
		fetch, _ := pager(100)
		items, err := FetchPagesConcurrently(pageSize, concurrency, fetch)
		require.NoError(t, err)
		assert.Len(t, items, 100)
	})

	t.Run("many pages are all collected in order", func(t *testing.T) {
		fetch, _ := pager(1350) // 13 full pages + a short one
		items, err := FetchPagesConcurrently(pageSize, concurrency, fetch)
		require.NoError(t, err)
		require.Len(t, items, 1350)
		for i := range items {
			assert.Equal(t, i, items[i])
		}
	})

	t.Run("never trusts a total: keeps paging while pages are full", func(t *testing.T) {
		// A source that reports nothing about totals; end is only inferable from
		// a short page. 250 items => pages at 0,100,200(short).
		fetch, _ := pager(250)
		items, err := FetchPagesConcurrently(pageSize, concurrency, fetch)
		require.NoError(t, err)
		assert.Len(t, items, 250)
	})

	t.Run("a page error aborts and propagates", func(t *testing.T) {
		fetch := func(offset int64) ([]int, error) {
			if offset == 0 {
				return make([]int, pageSize), nil // full, so it fans out
			}
			return nil, assert.AnError
		}
		_, err := FetchPagesConcurrently(pageSize, concurrency, fetch)
		assert.ErrorIs(t, err, assert.AnError)
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
				// The generated client returns a non-nil error alongside the 429
				// response; the retry must not bail on err alone.
				return "", &http.Response{StatusCode: 429, Header: header}, assert.AnError
			}
		})
		assert.Equal(t, "lol", res)
		assert.NoError(t, err)
		assert.Equal(t, 2, called)
	})

	t.Run("it returns the error without panicking on a nil response", func(t *testing.T) {
		called := 0
		_, err := Retry429s(func() (string, *http.Response, error) {
			called++
			return "", nil, assert.AnError
		})
		assert.ErrorIs(t, err, assert.AnError)
		assert.Equal(t, 1, called)
	})
}
