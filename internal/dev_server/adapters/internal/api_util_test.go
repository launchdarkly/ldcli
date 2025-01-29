package internal

import (
	"context"
	"net/http"
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
	items []testItem
	links map[string]ldapi.Link
}

func (r testResult) GetItems() []testItem {
	return r.items
}

func (r testResult) GetLinks() map[string]ldapi.Link {
	return r.links
}

func TestGetPaginatedItems(t *testing.T) {
	ctx := context.Background()
	projectKey := "test-project"

	testCases := []struct {
		name           string
		fetchResponses []testResult
		expectedItems  []testItem
		expectedError  bool
	}{
		{
			name: "Single page",
			fetchResponses: []testResult{
				{
					items: []testItem{{ID: "1"}, {ID: "2"}},
					links: map[string]ldapi.Link{},
				},
			},
			expectedItems: []testItem{{ID: "1"}, {ID: "2"}},
		},
		{
			name: "Multiple pages",
			fetchResponses: []testResult{
				{
					items: []testItem{{ID: "1"}, {ID: "2"}},
					links: map[string]ldapi.Link{
						"next": {Href: strPtr("http://example.com?limit=2&offset=2")},
					},
				},
				{
					items: []testItem{{ID: "3"}, {ID: "4"}},
					links: map[string]ldapi.Link{},
				},
			},
			expectedItems: []testItem{{ID: "1"}, {ID: "2"}, {ID: "3"}, {ID: "4"}},
		},
		{
			name: "Error on second page",
			fetchResponses: []testResult{
				{
					items: []testItem{{ID: "1"}, {ID: "2"}},
					links: map[string]ldapi.Link{
						"next": {Href: strPtr("http://example.com?limit=2&offset=2")},
					},
				},
			},
			expectedError: true,
		},
		{
			name: "Empty response",
			fetchResponses: []testResult{
				{
					items: []testItem{},
					links: map[string]ldapi.Link{},
				},
			},
			expectedItems: []testItem{},
		},
		{
			name: "Multiple pages with varying item counts",
			fetchResponses: []testResult{
				{
					items: []testItem{{ID: "1"}, {ID: "2"}, {ID: "3"}},
					links: map[string]ldapi.Link{
						"next": {Href: strPtr("http://example.com?limit=3&offset=3")},
					},
				},
				{
					items: []testItem{{ID: "4"}, {ID: "5"}},
					links: map[string]ldapi.Link{
						"next": {Href: strPtr("http://example.com?limit=3&offset=5")},
					},
				},
				{
					items: []testItem{{ID: "6"}},
					links: map[string]ldapi.Link{},
				},
			},
			expectedItems: []testItem{{ID: "1"}, {ID: "2"}, {ID: "3"}, {ID: "4"}, {ID: "5"}, {ID: "6"}},
		},
		{
			name: "Invalid next link",
			fetchResponses: []testResult{
				{
					items: []testItem{{ID: "1"}, {ID: "2"}},
					links: map[string]ldapi.Link{
						"next": {Href: strPtr("invalid-url")},
					},
				},
			},
			expectedError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			callCount := 0
			fetchFunc := func(ctx context.Context, projectKey string, limit, offset *int64) (testResult, error) {
				if callCount >= len(tc.fetchResponses) {
					return testResult{}, assert.AnError
				}
				result := tc.fetchResponses[callCount]
				callCount++
				return result, nil
			}

			items, err := GetPaginatedItems(ctx, projectKey, nil, fetchFunc)

			if tc.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expectedItems, items)
			}
		})
	}
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

func strPtr(s string) *string {
	return &s
}
