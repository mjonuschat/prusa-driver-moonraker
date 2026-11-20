package moonraker

import (
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// gcodesPath is the part of a storage-prefixed path that follows gcodes/, with
// repeated and surrounding slashes removed. The gcodes root itself gives "".
// Anything that leaves the root or names another storage is PathNotAllowed.
func gcodesPath(path string) (relative string, err error) {
	if err = connectorapi.RefusePathEscape(path); err != nil {
		return "", err
	}

	segments := make([]string, 0, 4)

	for segment := range strings.SplitSeq(path, "/") {
		if segment != "" {
			segments = append(segments, segment)
		}
	}

	if len(segments) == 0 || segments[0] != gcodesRoot {
		return "", connectorapi.PathNotAllowed.
			New("the path is not on the gcodes storage").
			With(fault.StringParam("path", path))
	}

	return strings.Join(segments[1:], "/"), nil
}

// hidden reports whether any segment of a path inside the gcodes root starts
// with a dot, which is the Unix convention for a file or folder nobody lists.
// Moonraker keeps its thumbnails in .thumbs, and Mainsail and Fluidd hide it
// too. The path is still reachable by name; it is only left out of listings and
// file events.
func hidden(relative string) bool {
	for _, segment := range strings.Split(relative, "/") {
		if strings.HasPrefix(segment, ".") && segment != "." && segment != ".." {
			return true
		}
	}

	return false
}
