package moonraker

import (
	"encoding/json"
	"fmt"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

var spoolPaths = []string{
	connectorapi.ToolPath(connectorapi.HeadID(0), connectorapi.MaterialAbbreviation),
	connectorapi.ToolPath(connectorapi.HeadID(0), connectorapi.MaterialType),
	connectorapi.ToolPath(connectorapi.HeadID(0), connectorapi.MaterialColor),
	connectorapi.ToolPath(connectorapi.HeadID(0), connectorapi.MaterialFullWeight),
	connectorapi.ToolPath(connectorapi.HeadID(0), connectorapi.ConsumedWeight),
}

// spoolReply holds the fields of a Spoolman spool this driver reads. Weights
// stay as encoded so a number is published unchanged.
type spoolReply struct {
	Filament struct {
		Material        string `json:"material"`
		ColorHex        string `json:"color_hex"`
		MultiColorHexes any    `json:"multi_color_hexes"`
	} `json:"filament"`
	InitialWeight json.RawMessage `json:"initial_weight"`
	UsedWeight    json.RawMessage `json:"used_weight"`
}

func decodeSpool(result json.RawMessage) (spoolReply, error) {
	var spool spoolReply
	if err := json.Unmarshal(result, &spool); err != nil {
		return spoolReply{}, fmt.Errorf("decode spool: %w", err)
	}

	return spool, nil
}

var nullValue = json.RawMessage(`null`)

func spoolPatches(spool spoolReply) []connectorapi.StatePatch {
	values := []json.RawMessage{
		materialValue(spool.Filament.Material),
		materialValue(spool.Filament.Material),
		colourValue(spool),
		weightValue(spool.InitialWeight),
		weightValue(spool.UsedWeight),
	}

	out := make([]connectorapi.StatePatch, len(spoolPaths))
	for i, path := range spoolPaths {
		out[i] = connectorapi.StatePatch{Path: path, Value: values[i]}
	}

	return out
}

func spoolClear() []connectorapi.StatePatch {
	out := make([]connectorapi.StatePatch, len(spoolPaths))
	for i, path := range spoolPaths {
		out[i] = connectorapi.StatePatch{Path: path, Value: nullValue}
	}

	return out
}

func materialValue(material string) json.RawMessage {
	if material == "" {
		return nullValue
	}
	encoded, _ := json.Marshal(material)

	return encoded
}

// colourValue refuses a multi-colour spool, whose primary colour is not known.
func colourValue(spool spoolReply) json.RawMessage {
	if multi, ok := spool.Filament.MultiColorHexes.(string); ok && multi != "" {
		return nullValue
	}
	text := "#" + spool.Filament.ColorHex
	if spool.Filament.ColorHex == "" || !connectorapi.ValidColor(text) {
		return nullValue
	}
	encoded, _ := json.Marshal(text)

	return encoded
}

func weightValue(raw json.RawMessage) json.RawMessage {
	var grams float64
	if len(raw) == 0 || json.Unmarshal(raw, &grams) != nil {
		return nullValue
	}
	if grams < 0 {
		return nullValue
	}

	return raw
}
