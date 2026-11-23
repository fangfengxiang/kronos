package main

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestGeneratedFileComparison(t *testing.T) {
	const generatedPath = "service/v1/example.pb.go"
	const content = "package example\n"
	regenerated := fstest.MapFS{generatedPath: {Data: []byte(content)}}
	testCases := []struct {
		name    string
		files   fstest.MapFS
		message string
	}{
		{name: "same files", files: fstest.MapFS{generatedPath: {Data: []byte(content)}}},
		{name: "missing file", files: fstest.MapFS{}, message: "generated file list differs"},
		{name: "different content", files: fstest.MapFS{generatedPath: {Data: []byte(content + "\n")}}, message: generatedPath},
		{
			name:    "extra file",
			files:   fstest.MapFS{generatedPath: {Data: []byte(content)}, "extra.go": {Data: []byte(content)}},
			message: "extra.go",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := compareGeneratedFiles(regenerated, testCase.files)
			if testCase.message == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), testCase.message) {
				t.Fatalf("file comparison error = %v, want %q", err, testCase.message)
			}
		})
	}
}
