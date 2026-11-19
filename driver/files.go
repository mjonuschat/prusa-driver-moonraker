package moonraker

import (
	"context"
	"fmt"
	"path"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

var _ connectorapi.Filesystem = (*Session)(nil)

// directoryReply is the part of server.files.get_directory this driver reads.
type directoryReply struct {
	Dirs []struct {
		Dirname  string  `json:"dirname"`
		Modified float64 `json:"modified"`
		Size     int64   `json:"size"`
	} `json:"dirs"`
	Files []struct {
		Filename string  `json:"filename"`
		Modified float64 `json:"modified"`
		Size     int64   `json:"size"`
	} `json:"files"`
	DiskUsage *diskUsage `json:"disk_usage"`
}

// List returns one directory of the gcodes storage. The empty path is the root
// and is answered from the handshake without a call.
func (s *Session) List(ctx context.Context, asked string) (connectorapi.Listing, error) {
	if asked == "" {
		return s.rootListing(), nil
	}

	relative, err := gcodesPath(asked)
	if err != nil {
		return connectorapi.Listing{}, err
	}

	if !s.caps.Has(connectorapi.VerbSendFileInfo) {
		return connectorapi.Listing{}, connectorapi.CapabilityNotSupported.
			New("the printer does not have that capability").
			With(fault.StringParam("verb", connectorapi.VerbSendFileInfo.String()))
	}

	folder := gcodesRoot
	if relative != "" {
		folder += "/" + relative
	}

	reply, refusal, err := s.directory(ctx, folder)
	if err != nil {
		return connectorapi.Listing{}, err
	}

	if refusal != nil {
		return s.fileOrRefusal(ctx, asked, folder, relative, refusal.Message)
	}

	readOnly := s.gcodesReadOnly()
	listing := connectorapi.Listing{
		Directory: connectorapi.File{
			Path: folder, Name: path.Base(folder), DisplayName: path.Base(folder),
			Folder: true, ReadOnly: readOnly,
		},
		Entries: make([]connectorapi.File, 0, len(reply.Dirs)+len(reply.Files)),
	}

	for _, dir := range reply.Dirs {
		listing.Entries = append(listing.Entries, connectorapi.File{
			Path: folder + "/" + dir.Dirname, Name: dir.Dirname, DisplayName: dir.Dirname,
			Folder: true, Size: dir.Size, Modified: int64(dir.Modified), ReadOnly: readOnly,
		})
	}

	for _, file := range reply.Files {
		listing.Entries = append(listing.Entries, connectorapi.File{
			Path: folder + "/" + file.Filename, Name: file.Filename, DisplayName: file.Filename,
			Size: file.Size, Modified: int64(file.Modified), ReadOnly: readOnly,
		})
	}

	if reply.DiskUsage != nil {
		s.setFreeSpace(*reply.DiskUsage)
	}

	return listing, nil
}

func (s *Session) rootListing() connectorapi.Listing {
	s.files.mu.Lock()
	defer s.files.mu.Unlock()

	listing := connectorapi.Listing{Directory: connectorapi.File{Folder: true}}

	for _, storage := range s.files.storages {
		listing.Entries = append(listing.Entries, connectorapi.File{
			Path: storage.Name, Name: storage.Name, Folder: true, ReadOnly: storage.ReadOnly,
		})
	}

	return listing
}

func (s *Session) gcodesReadOnly() bool {
	s.files.mu.Lock()
	defer s.files.mu.Unlock()

	return len(s.files.storages) > 0 && s.files.storages[0].ReadOnly
}

// directory asks for one folder. refusal is the machine's own error.
func (s *Session) directory(ctx context.Context, folder string) (*directoryReply, *rpcError, error) {
	result, refusal, err := s.call(ctx, methodGetDirectory, map[string]any{"path": folder, "extended": true})
	if err != nil || refusal != nil {
		return nil, refusal, err
	}

	var reply directoryReply
	if err := decodeResult(methodGetDirectory, result, &reply); err != nil {
		return nil, nil, err
	}

	return &reply, nil, nil
}

// fileOrRefusal answers a refused folder listing. A path that names a file in
// its parent is a file listing, and anything else is ListingRefused with the
// machine's first message.
func (s *Session) fileOrRefusal(
	ctx context.Context, asked, folder, relative, message string,
) (connectorapi.Listing, error) {
	if relative != "" {
		parent, refusal, err := s.directory(ctx, path.Dir(folder))
		if err != nil {
			return connectorapi.Listing{}, err
		}

		if refusal == nil {
			for _, file := range parent.Files {
				if file.Filename != path.Base(folder) {
					continue
				}

				return connectorapi.Listing{Directory: connectorapi.File{
					Path: folder, Name: file.Filename, DisplayName: file.Filename,
					Size: file.Size, Modified: int64(file.Modified), ReadOnly: s.gcodesReadOnly(),
				}}, nil
			}
		}
	}

	refused := connectorapi.ListingRefused.
		New(message).
		With(fault.StringParam("path", asked), fault.StringParam("machine_reason", message))

	return connectorapi.Listing{}, fmt.Errorf("%w: %w", connectorapi.ErrListingRefused, refused)
}
