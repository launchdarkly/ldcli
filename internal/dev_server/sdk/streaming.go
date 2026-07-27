package sdk

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/pkg/errors"
)

type MessageType string

const (
	TYPE_PUT   MessageType = "put"
	TYPE_PATCH MessageType = "patch"
)

type Message struct {
	Event MessageType
	Data  []byte
}

func (m Message) ToPayload() []byte {
	payload := []byte(fmt.Sprintf("event:%s\ndata:", m.Event))
	payload = append(payload, m.Data...)
	payload = append(payload, "\n\n"...)
	return payload
}

// OpenStream sets SSE headers, writes initialPayload, and starts the SSE loop.
// Each []byte sent to the returned channel is written verbatim to the response.
func OpenStream(w http.ResponseWriter, done <-chan struct{}, initialPayload []byte) (chan<- []byte, <-chan error) {
	errChan := make(chan error)
	updateChan := make(chan []byte, 10)
	go func() {
		var err error
		defer func() {
			errChan <- err
			close(errChan)
		}()
		err = func() error {
			flusher, ok := w.(http.Flusher)
			if !ok {
				return errors.New("expected http.ResponseWriter to be an http.Flusher")
			}

			w.Header().Set("Content-Type", "text/event-stream")
			_, err = w.Write(initialPayload)
			if err != nil {
				return errors.Wrap(err, "unable to write response")
			}
			flusher.Flush()
			ticker := time.NewTicker(time.Minute)
		loop:
			for {
				select {
				case <-ticker.C:
					_, err = w.Write([]byte(":\n\n"))
					if err != nil {
						return errors.Wrap(err, "unable to write response")
					}
					flusher.Flush()
				case payload := <-updateChan:
					_, err = w.Write(payload)
					if err != nil {
						return errors.Wrap(err, "unable to write response")
					}
					flusher.Flush()
				case <-done:
					break loop
				}
			}
			return nil
		}()
	}()
	return updateChan, errChan
}

func SendMessage(
	updateChan chan<- []byte,
	msgType MessageType,
	data interface{},
) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}

	updateChan <- Message{
		Event: msgType,
		Data:  payload,
	}.ToPayload()

	return nil
}
