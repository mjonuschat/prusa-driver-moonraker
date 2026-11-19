package moonraker

import (
	"context"
	"fmt"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

var _ connectorapi.FileManager = (*Session)(nil)

func (s *Session) CreateFolder(ctx context.Context, path string) error {
	return s.change(ctx, connectorapi.VerbCreateFolder, methodPostDirectory, path)
}

func (s *Session) DeleteFile(ctx context.Context, path string) error {
	return s.change(ctx, connectorapi.VerbDeleteFile, methodDeleteFile, path)
}

// DeleteFolder removes one folder under gcodes. No force flag is sent, so
// Moonraker refuses a folder that still holds files.
func (s *Session) DeleteFolder(ctx context.Context, path string) error {
	return s.change(ctx, connectorapi.VerbDeleteFolder, methodDeleteDirectory, path)
}

func (s *Session) change(ctx context.Context, verb connectorapi.Capability, method, asked string) error {
	if !s.caps.Has(verb) {
		return connectorapi.CapabilityNotSupported.
			New("the printer does not have that capability").
			With(fault.StringParam("verb", verb.String()))
	}

	relative, err := gcodesPath(asked)
	if err != nil {
		return err
	}

	if relative == "" {
		return connectorapi.PathNotAllowed.
			New("the gcodes root itself cannot be changed").
			With(fault.StringParam("path", asked))
	}

	_, refusal, err := s.call(ctx, method, map[string]any{"path": gcodesRoot + "/" + relative})
	if err != nil {
		return err
	}

	if refusal != nil {
		refused := connectorapi.FileChangeRefused.
			New("the printer refused that change to its storage").
			With(fault.StringParam("path", asked), fault.StringParam("machine_reason", refusal.Message))

		return fmt.Errorf("%w: %w", connectorapi.ErrFileChangeRefused, refused)
	}

	return nil
}
