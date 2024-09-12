package adapters

import (
	"context"
	"testing"

	ldapi "github.com/launchdarkly/api-client-go/v14"
	"github.com/stretchr/testify/assert"
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

			items, err := getPaginatedItems(ctx, projectKey, nil, fetchFunc)

			if tc.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expectedItems, items)
			}
		})
	}
}

func strPtr(s string) *string {
	return &s
}
