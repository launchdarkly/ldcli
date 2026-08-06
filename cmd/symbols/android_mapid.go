package symbols

import (
	"bufio"
	"os"
	"regexp"
	"strings"
)

// The id R8 gives the mapping it produced.
//
// R8 writes "# pg_map_id: <hash of the mapping>" into every mapping's header, and
// from AGP 8.12 it stamps that same id into each class's source file attribute — so
// a shipped app reports "r8-map-id-<hash>" where a file name goes, on every frame of
// every crash. Keying the upload by it puts the index exactly where symbolication
// will look for a build that was asked to do nothing at all.
//
// It is read out of the header rather than recomputed here because the header is
// R8's own statement of what it stamped. A hash computed on this side would have to
// agree with R8's forever, and would be silently wrong the first time it didn't.

// androidMapIDComment is the mapping header line carrying the id.
const androidMapIDComment = "# pg_map_id:"

// androidMapIDPattern is the shape of an id that may be keyed by: a hash, which is
// the mapping's full SHA-256 from AGP 8.12 and a 7-character prefix of it before.
var androidMapIDPattern = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

// androidMapID returns the id R8 recorded for a mapping, or "" when it recorded none
// — a mapping written by ProGuard rather than R8, or one old enough to predate the
// header. Best effort, like every other way of learning what a build shipped: an
// upload with no id falls back to the Version Lane rather than failing over it.
func androidMapID(mappingPath string) string {
	file, err := os.Open(mappingPath)
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		// The header is the comments before the first class, and a release mapping
		// is tens of megabytes of what follows it.
		if !strings.HasPrefix(line, "#") {
			return ""
		}
		rest, ok := strings.CutPrefix(line, androidMapIDComment)
		if !ok {
			continue
		}
		if id := strings.TrimSpace(rest); androidMapIDPattern.MatchString(id) {
			return id
		}
		return ""
	}
	return ""
}
