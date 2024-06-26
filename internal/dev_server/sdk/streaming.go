package sdk

import (
	"fmt"
	"net/http"
	"time"

	"github.com/pkg/errors"
)

type Message struct {
	Event string
	Data  []byte
}

func (m Message) ToPayload() []byte {
	payload := []byte(fmt.Sprintf("event:%s\ndata:", m.Event))
	payload = append(payload, m.Data...)
	payload = append(payload, "\n\n"...)
	return payload
}

// OpenStream sends data to a response using the initial payload and subsequently via the returned write only channel
func OpenStream(w http.ResponseWriter, done <-chan struct{}, initialMessage Message) (chan<- Message, <-chan error) {
	errChan := make(chan error)
	updateChan := make(chan Message, 10)
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
			_, err = w.Write(initialMessage.ToPayload())
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
				case <-updateChan:
					// TODO do stuff with updateChan
				case <-done:
					break loop
				}
			}
			return nil
		}()
	}()
	return updateChan, errChan
}
