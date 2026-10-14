package narrate

import (
	"encoding/json"
	"fmt"
	"io"
)

// WriteJSON writes m as indented JSON, without HTML escaping.
func WriteJSON(w io.Writer, m *Module) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		return fmt.Errorf("writing json module report: %w", err)
	}
	return nil
}
