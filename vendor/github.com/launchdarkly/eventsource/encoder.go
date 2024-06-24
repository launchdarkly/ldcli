package eventsource

import (
	"compress/gzip"
	"fmt"
	"io"
	"strings"
)

var (
	encFields = []struct { //nolint:gochecknoglobals // non-exported global that we treat as a constant
		prefix string
		value  func(Event) string
	}{
		{"id: ", Event.Id},
		{"event: ", Event.Event},
		{"data: ", Event.Data},
	}
)

// An Encoder is capable of writing Events to a stream. Optionally
// Events can be gzip compressed in this process.
type Encoder struct {
	w          io.Writer
	compressed bool
}

// NewEncoder returns an Encoder for a given io.Writer.
// When compressed is set to true, a gzip writer will be
// created.
func NewEncoder(w io.Writer, compressed bool) *Encoder {
	if compressed {
		return &Encoder{w: gzip.NewWriter(w), compressed: true}
	}
	return &Encoder{w: w}
}

// Encode writes an event or comment in the format specified by the
// server-sent events protocol.
func (enc *Encoder) Encode(ec eventOrComment) error {
	switch item := ec.(type) {
	case Event:
		for _, field := range encFields {
			prefix, value := field.prefix, field.value(item)
			if len(value) == 0 {
				continue
			}
			value = strings.Replace(value, "\n", "\n"+prefix, -1)
			if _, err := io.WriteString(enc.w, prefix+value+"\n"); err != nil {
				return fmt.Errorf("eventsource encode: %v", err)
			}
		}
		if _, err := io.WriteString(enc.w, "\n"); err != nil {
			return fmt.Errorf("eventsource encode: %v", err)
		}
	case comment:
		line := ":" + item.value + "\n"
		if _, err := io.WriteString(enc.w, line); err != nil {
			return fmt.Errorf("eventsource encode: %v", err)
		}
	default:
		return fmt.Errorf("unexpected parameter to Encode: %v", ec)
	}
	if enc.compressed {
		return enc.w.(*gzip.Writer).Flush()
	}
	return nil
}
