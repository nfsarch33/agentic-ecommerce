package main

import (
	"encoding/json"
	"io"
)

// encodeJSON marshals v as indented JSON to w. Used by --json output
// modes across subcommands so the shape stays consistent.
func encodeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
