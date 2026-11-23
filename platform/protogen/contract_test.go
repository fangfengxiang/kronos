package main

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

const publicEcho = "example.com/services/echo"

func TestGenerateContractFromMultipleFiles(t *testing.T) {
	request := echoDescriptors()
	plugin, err := (protogen.Options{}).New(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := generateContract(plugin, publicEcho); err != nil {
		t.Fatal(err)
	}
	result := plugin.Response()
	if result.GetError() != "" || len(result.File) != 1 {
		t.Fatalf("unexpected generator result: %v", result)
	}
	file := result.File[0]
	if file.GetName() != "echo/contract.kronos.pb.go" {
		t.Fatalf("generated filename = %q", file.GetName())
	}
	for _, expected := range []string{
		"package echo",
		"type Request = v1.Request",
		"type Response = v1.Response",
		"type Response_Text = v1.Response_Text",
		"type Status = v1.Status",
		"const Status_STATUS_UNSPECIFIED = v1.Status_STATUS_UNSPECIFIED",
		"Echo(context.Context, *Request) (*Response, error)",
	} {
		if !strings.Contains(file.GetContent(), expected) {
			t.Errorf("generated contract lacks %q:\n%s", expected, file.GetContent())
		}
	}
	for _, excluded := range []string{"Watch(context.Context", "type Request_OptionalText"} {
		if strings.Contains(file.GetContent(), excluded) {
			t.Errorf("generated contract unexpectedly has %q:\n%s", excluded, file.GetContent())
		}
	}
}

func TestRejectsIncompatibleContractInputs(t *testing.T) {
	t.Run("public package outside the private owner", func(t *testing.T) {
		plugin, err := (protogen.Options{}).New(echoDescriptors())
		if err != nil {
			t.Fatal(err)
		}
		err = generateContract(plugin, "example.com/services/other")
		if err == nil || !strings.Contains(err.Error(), "messages.proto: go_package") {
			t.Fatalf("unexpected generation error: %v", err)
		}
	})
	t.Run("duplicate public alias", func(t *testing.T) {
		request := echoDescriptors()
		duplicate := &descriptorpb.FileDescriptorProto{
			Name:    proto.String("other.proto"),
			Package: proto.String("other.v1"),
			Syntax:  proto.String("proto3"),
			Options: &descriptorpb.FileOptions{GoPackage: proto.String(publicEcho + "/internal/wire/other")},
			MessageType: []*descriptorpb.DescriptorProto{
				{Name: proto.String("Response")},
			},
		}
		request.ProtoFile = append(request.ProtoFile, duplicate)
		request.FileToGenerate = append(request.FileToGenerate, duplicate.GetName())
		plugin, err := (protogen.Options{}).New(request)
		if err != nil {
			t.Fatal(err)
		}
		err = generateContract(plugin, publicEcho)
		if err == nil || !strings.Contains(err.Error(), "other.proto: duplicate public Go declaration Response") {
			t.Fatalf("unexpected generation error: %v", err)
		}
	})
	t.Run("unpublished private signature type", func(t *testing.T) {
		request := echoDescriptors()
		request.FileToGenerate = []string{"service.proto"}
		plugin, err := (protogen.Options{}).New(request)
		if err != nil {
			t.Fatal(err)
		}
		err = generateContract(plugin, publicEcho)
		if err == nil || !strings.Contains(err.Error(), "private type echo.v1.Request") {
			t.Fatalf("unexpected generation error: %v", err)
		}
	})
}

func echoDescriptors() *pluginpb.CodeGeneratorRequest {
	options := &descriptorpb.FileOptions{GoPackage: proto.String(publicEcho + "/internal/wire/v1")}
	messages := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("messages.proto"),
		Package: proto.String("echo.v1"),
		Syntax:  proto.String("proto3"),
		Options: options,
		EnumType: []*descriptorpb.EnumDescriptorProto{{
			Name:  proto.String("Status"),
			Value: []*descriptorpb.EnumValueDescriptorProto{{Name: proto.String("STATUS_UNSPECIFIED"), Number: proto.Int32(0)}},
		}},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:      proto.String("Request"),
				OneofDecl: []*descriptorpb.OneofDescriptorProto{{Name: proto.String("_optional_text")}},
				Field: []*descriptorpb.FieldDescriptorProto{{
					Name: proto.String("optional_text"), Number: proto.Int32(1),
					Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
					OneofIndex: proto.Int32(0), Proto3Optional: proto.Bool(true),
				}},
			},
			{
				Name:      proto.String("Response"),
				OneofDecl: []*descriptorpb.OneofDescriptorProto{{Name: proto.String("payload")}},
				Field: []*descriptorpb.FieldDescriptorProto{{
					Name: proto.String("text"), Number: proto.Int32(1), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(), OneofIndex: proto.Int32(0),
				}},
			},
		},
	}
	service := &descriptorpb.FileDescriptorProto{
		Name:       proto.String("service.proto"),
		Package:    proto.String("echo.v1"),
		Syntax:     proto.String("proto3"),
		Options:    options,
		Dependency: []string{"messages.proto"},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: proto.String("Echo"),
			Method: []*descriptorpb.MethodDescriptorProto{
				{Name: proto.String("Echo"), InputType: proto.String(".echo.v1.Request"), OutputType: proto.String(".echo.v1.Response")},
				{Name: proto.String("Watch"), InputType: proto.String(".echo.v1.Request"), OutputType: proto.String(".echo.v1.Response"), ServerStreaming: proto.Bool(true)},
			},
		}},
	}
	return &pluginpb.CodeGeneratorRequest{
		Parameter:      proto.String("module=example.com/services"),
		ProtoFile:      []*descriptorpb.FileDescriptorProto{messages, service},
		FileToGenerate: []string{"service.proto", "messages.proto"},
	}
}
