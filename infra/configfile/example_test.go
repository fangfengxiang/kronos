package configfile_test

import (
	"fmt"
	"os"

	"github.com/fangfengxiang/kronos/infra/configfile"
)

func ExampleLoadJSON() {
	type listenConfig struct {
		HTTPAddress string `json:"http"`
	}
	file, err := os.CreateTemp("", "assistant-config-*.json")
	if err != nil {
		panic(err)
	}
	defer os.Remove(file.Name())
	_, writeErr := file.WriteString(`{"http":"127.0.0.1:8080"}`)
	if closeErr := file.Close(); closeErr != nil {
		panic(closeErr)
	}
	if writeErr != nil {
		panic(writeErr)
	}
	snapshot, err := configfile.LoadJSON[listenConfig](file.Name())
	if err != nil {
		panic(err)
	}
	fmt.Println(snapshot.HTTPAddress)
	// Output: 127.0.0.1:8080
}
