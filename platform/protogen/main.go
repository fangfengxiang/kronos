// Command protogen generates public Kronos contracts from Protobuf descriptors.
package main

import (
	"flag"
	"os"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/types/pluginpb"
)

func main() {
	flags := flag.NewFlagSet("protoc-gen-kronos", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	apiImport := flags.String("api_import", "", "public Go import path (default: owner of internal/wire in go_package)")
	unaryBindings := flags.Bool("unary_bindings", false, "generate bindings for the Kronos unary runtime")
	options := protogen.Options{ParamFunc: flags.Set}
	options.Run(func(plugin *protogen.Plugin) error {
		plugin.SupportedFeatures = uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)
		return generateContract(plugin, *apiImport, *unaryBindings)
	})
}
