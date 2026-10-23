package adapters

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	ldsdk "github.com/launchdarkly/go-server-sdk/v7"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSdkInitTimeout(t *testing.T) {
	t.Run("uses the provided timeout", func(t *testing.T) {
		s, ok := newSdk("", 15*time.Second).(streamingSdk)
		require.True(t, ok)
		assert.Equal(t, 15*time.Second, s.initTimeout)
	})

	t.Run("falls back to the default when zero", func(t *testing.T) {
		s, ok := newSdk("", 0).(streamingSdk)
		require.True(t, ok)
		assert.Equal(t, DefaultSdkInitTimeout, s.initTimeout)
	})

	t.Run("falls back to the default when negative", func(t *testing.T) {
		s, ok := newSdk("", -time.Second).(streamingSdk)
		require.True(t, ok)
		assert.Equal(t, DefaultSdkInitTimeout, s.initTimeout)
	})
}

func TestGetAllFlagsStateClosesClientWhenInitFails(t *testing.T) {
	t.Run("rejected SDK key", func(t *testing.T) {
		requests, url := newStreamServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		})

		logs, err := getAllFlagsStateFailure(t, url)

		assertInitTimeout(t, err)
		assert.Contains(t, logs.String(), "Closing LaunchDarkly client")
		assert.Equal(t, int32(1), requests.Load())
	})

	t.Run("stream that sends no data", func(t *testing.T) {
		streamClosed := make(chan struct{})
		testDone := make(chan struct{})
		_, url := newStreamServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				close(streamClosed)
			case <-testDone:
			}
		})
		t.Cleanup(func() { close(testDone) })

		logs, err := getAllFlagsStateFailure(t, url)

		assertInitTimeout(t, err)
		assert.Contains(t, logs.String(), "Closing LaunchDarkly client")
		select {
		case <-streamClosed:
		case <-time.After(2 * time.Second):
			t.Fatal("the SDK client kept the stream open after GetAllFlagsState returned")
		}
	})

	t.Run("stream that is unavailable", func(t *testing.T) {
		requests, url := newStreamServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		})

		logs, err := getAllFlagsStateFailure(t, url)

		assertInitTimeout(t, err)
		assert.Contains(t, logs.String(), "Closing LaunchDarkly client")
		before := requests.Load()
		require.Positive(t, before)
		// The SDK retries a 503 after about one second, so a client that is still open sends more requests.
		time.Sleep(2 * time.Second)
		assert.Equal(t, before, requests.Load(), "the SDK client kept retrying after GetAllFlagsState returned")
	})
}

func newStreamServer(t *testing.T, handler http.HandlerFunc) (*atomic.Int32, string) {
	t.Helper()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	return &requests, server.URL
}

func getAllFlagsStateFailure(t *testing.T, streamingUrl string) (*syncBuffer, error) {
	t.Helper()
	logs := &syncBuffer{}
	s := streamingSdk{
		streamingUrl: streamingUrl,
		initTimeout:  500 * time.Millisecond,
		logger:       log.New(logs, "", 0),
	}
	_, err := s.GetAllFlagsState(context.Background(), ldcontext.New("test-user"), "sdk-key")
	return logs, err
}

func assertInitTimeout(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	assert.ErrorIs(t, err, ldsdk.ErrInitializationTimeout)
	assert.EqualError(t, err, "unable to get source flags from LD SDK: timeout encountered waiting for LaunchDarkly client initialization")
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
