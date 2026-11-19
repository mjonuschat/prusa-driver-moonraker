package moonraker

import (
	"context"
	"encoding/json"
	"net/url"
	"path"
	"strings"
)

// previewCap is the largest thumbnail, in bytes, that is fetched and reported.
const previewCap = 512 * 1024

// thumbnail is one entry of the thumbnail list in a file's metadata.
type thumbnail struct {
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	Size         int    `json:"size"`
	RelativePath string `json:"relative_path"`
}

// metadataReply is the part of server.files.metadata this driver reads.
type metadataReply struct {
	Size       int64       `json:"size"`
	Modified   float64     `json:"modified"`
	Thumbnails []thumbnail `json:"thumbnails"`
}

// pickThumbnail takes the PNG with the most pixels among those no larger than
// previewCap. The first of equals wins.
func pickThumbnail(list []thumbnail) (thumbnail, bool) {
	var best thumbnail

	found := false

	for _, candidate := range list {
		if !strings.HasSuffix(strings.ToLower(candidate.RelativePath), ".png") || candidate.Size > previewCap {
			continue
		}

		if !found || candidate.Width*candidate.Height > best.Width*best.Height {
			best, found = candidate, true
		}
	}

	return best, found
}

// thumbnailURL is where Moonraker serves a thumbnail, under the file's
// directory in the gcodes root with each segment escaped.
func thumbnailURL(base, file, relative string) string {
	var segments []string

	if dir := path.Dir(file); dir != "." {
		segments = append(segments, strings.Split(dir, "/")...)
	}

	segments = append(segments, strings.Split(relative, "/")...)

	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}

	return base + "/server/files/" + gcodesRoot + "/" + strings.Join(segments, "/")
}

// metadataOf asks for a file's metadata. It is nil when the machine refuses or
// the answer is unreadable.
func (s *Session) metadataOf(ctx context.Context, relative string) *metadataReply {
	result, refusal, err := s.call(ctx, methodGetMetadata, map[string]any{"filename": relative})
	if err != nil || refusal != nil {
		return nil
	}

	var reply metadataReply
	if json.Unmarshal(result, &reply) != nil {
		return nil
	}

	return &reply
}

// previewFrom is the encoded picture named by a file's metadata, or nil on every failure.
func (s *Session) previewFrom(ctx context.Context, relative string, reply *metadataReply) []byte {
	chosen, ok := pickThumbnail(reply.Thumbnails)
	if !ok {
		return nil
	}

	fetchCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	body, err := s.fetch.get(fetchCtx, thumbnailURL(s.httpBase, relative, chosen.RelativePath), previewCap)
	if err != nil {
		s.log.Debug("the preview could not be fetched", "error", err.Error())

		return nil
	}

	return body
}
