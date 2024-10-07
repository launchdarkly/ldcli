package internal

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/launchdarkly/api-client-go/v14"
	"github.com/pkg/errors"
)

func GetPaginatedItems[T any, R interface {
	GetItems() []T
	GetLinks() map[string]ldapi.Link
}](ctx context.Context, projectKey string, href *string, fetchFunc func(context.Context, string, *int64, *int64) (R, error)) ([]T, error) {
	var result R
	var err error

	if href == nil {
		result, err = fetchFunc(ctx, projectKey, nil, nil)
		if err != nil {
			return nil, err
		}
	} else {
		limit, offset, err := parseHref(*href)
		if err != nil {
			return nil, errors.Wrapf(err, "unable to parse href for next link: %s", *href)
		}
		result, err = fetchFunc(ctx, projectKey, &limit, &offset)
		if err != nil {
			return nil, err
		}
	}

	items := result.GetItems()

	if links := result.GetLinks(); links != nil {
		if next, ok := links["next"]; ok && next.Href != nil {
			newItems, err := GetPaginatedItems(ctx, projectKey, next.Href, fetchFunc)
			if err != nil {
				return nil, err
			}
			items = append(items, newItems...)
		}
	}

	return items, nil
}

func parseHref(href string) (limit, offset int64, err error) {
	parsedUrl, err := url.Parse(href)
	if err != nil {
		return
	}
	l, err := strconv.Atoi(parsedUrl.Query().Get("limit"))
	if err != nil {
		return
	}
	o, err := strconv.Atoi(parsedUrl.Query().Get("offset"))
	if err != nil {
		return
	}

	limit = int64(l)
	offset = int64(o)
	return
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
