package main

import (
	"flag"
	"fmt"
	"maps"
	"slices"

	"github.com/fangfengxiang/kronos/infra/configfile"
)

const (
	configPathFlag    = "config"
	reloadPIDFileFlag = "reload-pid-file"
	reloadTimeoutFlag = "reload-timeout"
)

func applyConfigFile(flags *flag.FlagSet, path string) error {
	fileOptions, err := configfile.LoadJSON[map[string]*string](path)
	if err != nil {
		return err
	}
	commandLineOptions := make(map[string]struct{})
	flags.Visit(func(option *flag.Flag) {
		commandLineOptions[option.Name] = struct{}{}
	})
	for _, name := range slices.Sorted(maps.Keys(fileOptions)) {
		switch name {
		case configPathFlag, reloadPIDFileFlag, reloadTimeoutFlag:
			return fmt.Errorf("config %q: option %q must be set on the command line", path, name)
		}
		if flags.Lookup(name) == nil {
			return fmt.Errorf("config %q: unknown option %q", path, name)
		}
		value := fileOptions[name]
		if value == nil {
			return fmt.Errorf("config %q: option %q must be a string", path, name)
		}
		if _, overridden := commandLineOptions[name]; overridden {
			continue
		}
		if err := flags.Set(name, *value); err != nil {
			return fmt.Errorf("config %q: option %q: %w", path, name, err)
		}
	}
	return nil
}
