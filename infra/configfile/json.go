// Package configfile loads local configuration snapshots.
package configfile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

// LoadJSON reads one JSON object into a fresh T. Struct fields are checked
// for unknown keys. On failure it returns the zero value of T and the error.
func LoadJSON[T any](path string) (T, error) {
	var zero T
	data, err := os.ReadFile(path)
	if err != nil {
		return zero, fmt.Errorf("read JSON config: %w", err)
	}

	data = bytes.Trim(data, " \r\n\t")
	if len(data) == 0 || data[0] != '{' {
		return zero, fmt.Errorf("decode JSON config %q: expected a JSON object", path)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var config T
	if err := decoder.Decode(&config); err != nil {
		return zero, fmt.Errorf("decode JSON config %q: %w", path, err)
	}
	if decoder.InputOffset() != int64(len(data)) {
		return zero, fmt.Errorf("decode JSON config %q: unexpected data after JSON object", path)
	}
	return config, nil
}
