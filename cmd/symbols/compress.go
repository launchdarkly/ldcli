package symbols

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
)

// Uploads are gzipped.
//
// A release R8 mapping is tens of megabytes of text, a React Native source map
// several, and every build sends one again; compressed, that is roughly an eighth
// of the bytes. The object is marked Content-Encoding: gzip, which keeps it
// self-describing: storage returns the header on read, an HTTP client inflates it
// in transit, and the symbolication reader inflates whatever still arrives
// compressed.
//
// Compression is settled before an upload URL is requested rather than at the point
// of sending, because the digest that proves an object is already stored has to be
// the digest of the bytes that get stored.

// gzipEncoding is the Content-Encoding an upload is marked with.
const gzipEncoding = "gzip"

// uploadBody is what a PUT sends: the bytes, and how they are encoded.
type uploadBody struct {
	Data     []byte
	Encoding string
	// RawSize is the artifact's size before compression, for reporting.
	RawSize int
}

// compressBody gzips an artifact held in memory, keeping it as it is when
// compressing gains nothing — a .srcbundle already gzips its own entries, and
// wrapping one again would only put a reader a step further from it.
func compressBody(data []byte) uploadBody {
	compressed, err := gzipBytes(data)
	if err != nil || len(compressed) >= len(data) {
		return uploadBody{Data: data, RawSize: len(data)}
	}
	return uploadBody{Data: compressed, Encoding: gzipEncoding, RawSize: len(data)}
}

func gzipBytes(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	if _, err := writer.Write(data); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// gzipFile compresses a file on disk, streaming it through the compressor so that a
// mapping large enough to be worth compressing is never held in memory whole.
func gzipFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	if _, err := io.Copy(writer, file); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// byteSize renders a size the way upload progress reports it.
func byteSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for rest := n / unit; rest >= unit; rest /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}
