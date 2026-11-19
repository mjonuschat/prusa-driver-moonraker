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
