package configfile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type listenerConfig struct {
	Address string `json:"address"`
}

func TestLoadJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "assistant.json")
	data := " \n{\"address\":\"127.0.0.1:8080\"}\r\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	config, err := LoadJSON[listenerConfig](path)
	if err != nil {
		t.Fatal(err)
	}
	if config.Address != "127.0.0.1:8080" {
		t.Fatalf("address = %q", config.Address)
	}
}

func TestLoadJSONRejectsInvalidSnapshots(t *testing.T) {
	testCases := []struct {
		name    string
		data    string
		message string
	}{
		{name: "empty", data: " \n", message: "expected a JSON object"},
		{name: "null", data: "null", message: "expected a JSON object"},
		{name: "array", data: `[]`, message: "expected a JSON object"},
		{name: "partial object", data: `{"address":"localhost:8080"`, message: "unexpected EOF"},
		{name: "unknown field", data: `{"address":"localhost:8080","typo":"x"}`, message: `unknown field "typo"`},
		{name: "another object", data: `{"address":"localhost:8080"} {}`, message: "unexpected data after JSON object"},
		{name: "trailing junk", data: `{"address":"localhost:8080"} error`, message: "unexpected data after JSON object"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "assistant.json")
			if err := os.WriteFile(path, []byte(testCase.data), 0600); err != nil {
				t.Fatal(err)
			}
			config, err := LoadJSON[listenerConfig](path)
			if err == nil || !strings.Contains(err.Error(), testCase.message) || !strings.Contains(err.Error(), strconv.Quote(path)) {
				t.Fatalf("LoadJSON error = %v, want %q and the file path", err, testCase.message)
			}
			if config != (listenerConfig{}) {
				t.Fatalf("LoadJSON returned a partial configuration: %+v", config)
			}
		})
	}
}

func TestLoadJSONPreservesReadErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	_, err := LoadJSON[listenerConfig](path)
	var pathError *fs.PathError
	if !errors.Is(err, fs.ErrNotExist) || !errors.As(err, &pathError) || pathError.Path != path {
		t.Fatalf("LoadJSON error = %v, want the original path error", err)
	}
}
