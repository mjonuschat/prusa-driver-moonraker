package moonraker

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// Cancelling one object of a print uses Klipper's exclude_object section.
const (
	VerbCancelObject   = connectorapi.Capability("core.command.cancel_object")
	ArgCancelObject    = connectorapi.Capability("core.command.cancel_object#id")
	VerbUncancelObject = connectorapi.Capability("core.command.uncancel_object")
	ArgUncancelObject  = connectorapi.Capability("core.command.uncancel_object#id")

	objectExcludeObject = "exclude_object"

	// maxObjectID is the largest id a cancel verb takes, as Prusa's own firmware
	// declares it.
	maxObjectID = 65535
)

func objectIDArgument() connectorapi.Value {
	return connectorapi.Value{
		Flag: true, Kind: connectorapi.KindInteger, Required: true,
		Min: 0, HasMin: true, Limit: maxObjectID, HasLimit: true,
	}
}

func objectsBaseline() connectorapi.Set {
	return connectorapi.Set{
		VerbCancelObject:   {Flag: true},
		ArgCancelObject:    objectIDArgument(),
		VerbUncancelObject: {Flag: true},
		ArgUncancelObject:  objectIDArgument(),
	}
}

func carryObjects(into, from connectorapi.Set, available []string) {
	if slices.Contains(available, objectExcludeObject) {
		carry(into, from, VerbCancelObject, ArgCancelObject, VerbUncancelObject, ArgUncancelObject)
	}
}

// objectBook is the object list of the print, as Klipper's exclude_object
// reports it. Moonraker sends only what changed, so the list and the excluded
// names arrive separately and each is remembered.
//
// An object's id is its index in Klipper's own list, which is sorted by name
// and complete before the print's first move. mu guards names, which a verb
// reads from another goroutine than the reader.
type objectBook struct {
	mu    sync.Mutex
	names []string

	heard     bool
	excluded  []string
	published string
}

// nameOf is the object with the given id, and how many objects there are.
func (b *objectBook) nameOf(id int) (name string, count int, found bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if id < 0 || id >= len(b.names) {
		return "", len(b.names), false
	}

	return b.names[id], len(b.names), true
}

// appendObjects writes the whole array at /job/objects when it changes, and
// nothing for a machine that does not report exclude_object.
func (b *objectBook) appendObjects(
	out []connectorapi.StatePatch, fields map[string]json.RawMessage,
) []connectorapi.StatePatch {
	if len(fields) == 0 {
		return out
	}

	if raw, present := fields["objects"]; present {
		var listed []struct {
			Name string `json:"name"`
		}

		if json.Unmarshal(raw, &listed) == nil {
			names := make([]string, 0, len(listed))
			for _, object := range listed {
				names = append(names, object.Name)
			}

			b.mu.Lock()
			b.names = names
			b.mu.Unlock()

			b.heard = true
		}
	}

	if raw, present := fields["excluded_objects"]; present {
		var names []string
		if json.Unmarshal(raw, &names) == nil {
			b.excluded = names
		}
	}

	if !b.heard {
		return out
	}

	b.mu.Lock()
	entries := make([]connectorapi.JobObject, 0, len(b.names))

	for id, name := range b.names {
		entries = append(entries, connectorapi.JobObject{
			ID: id, Name: name, Cancelled: slices.Contains(b.excluded, name),
		})
	}
	b.mu.Unlock()

	encoded, err := json.Marshal(entries)
	if err != nil || string(encoded) == b.published {
		return out
	}

	b.published = string(encoded)

	return append(out, connectorapi.StatePatch{Path: connectorapi.JobObjectsPath, Value: encoded})
}

// objectScript is the gcode that cancels an object or brings it back. Bringing
// one back can fail a print whose layers were already skipped, which Klipper
// documents, so its own answer is returned and not second-guessed.
func objectScript(name string, cancel bool) (string, error) {
	if name == "" || strings.ContainsFunc(name, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r) || r == ';'
	}) {
		return "", malformed(name, "the object's name cannot be written into one gcode command")
	}

	if cancel {
		return "EXCLUDE_OBJECT NAME=" + name, nil
	}

	return "EXCLUDE_OBJECT RESET=1 NAME=" + name, nil
}

// objectCommand is the script for one cancel or uncancel verb.
func (s *Session) objectCommand(cmd connectorapi.Command) (string, error) {
	id, err := strconv.Atoi(cmd.Args["id"])
	if err != nil {
		return "", connectorapi.ArgumentMalformed.New("the object id is not a whole number").
			With(fault.StringParam("verb", cmd.Verb.String()), fault.StringParam("argument", "id"))
	}

	name, count, found := s.reader.objects.nameOf(id)
	if !found {
		return "", connectorapi.ArgumentOutOfRange.New("the print has no object with that id").
			With(fault.StringParam("verb", cmd.Verb.String()), fault.StringParam("argument", "id"),
				fault.StringParam("max", strconv.Itoa(max(count-1, 0))))
	}

	return objectScript(name, cmd.Verb == VerbCancelObject)
}
