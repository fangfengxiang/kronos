package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestUnaryHTTPRemapsGETQuery(t *testing.T) {
	method := httpTestMethod(t, &annotations.HttpRule{Pattern: &annotations.HttpRule_Get{Get: "/v1/lookup"}})
	server, err := UnaryHTTPServer(nil, []Service{{Name: "test.Query", Methods: []Method{method}}})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	server.(http.Handler).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/lookup?value=warehouse", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "warehouse") {
		t.Fatalf("HTTP query status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestUnaryHTTPRejectsAmbiguousOrUnsupportedMappings(t *testing.T) {
	for _, test := range []struct {
		name, want string
		rule       *annotations.HttpRule
		copies     int
	}{
		{name: "missing routes", want: "at least one annotated", copies: 1},
		{name: "path template", want: "static path", rule: &annotations.HttpRule{Pattern: &annotations.HttpRule_Get{Get: "/v1/{value}"}}, copies: 1},
		{name: "field body", want: "GET without a body or POST", rule: &annotations.HttpRule{Pattern: &annotations.HttpRule_Post{Post: "/v1/lookup"}, Body: "value"}, copies: 1},
		{name: "duplicate route", want: "is shared", rule: &annotations.HttpRule{Pattern: &annotations.HttpRule_Get{Get: "/v1/lookup"}}, copies: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			first := Service{Name: "test.Query", Methods: []Method{httpTestMethod(t, test.rule)}}
			services := []Service{first}
			if test.copies > 1 {
				services = append(services, Service{Name: "test.Other", Methods: first.Methods})
			}
			if _, err := UnaryHTTPServer(nil, services); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("HTTP assembly = %v, want %q", err, test.want)
			}
		})
	}
}

func httpTestMethod(t *testing.T, rule *annotations.HttpRule) Method {
	t.Helper()
	options := &descriptorpb.MethodOptions{}
	if rule != nil {
		proto.SetExtension(options, annotations.E_Http, rule)
	}
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name: proto.String("test/query.proto"), Package: proto.String("test"), Syntax: proto.String("proto3"),
		Dependency: []string{"google/protobuf/wrappers.proto"},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: proto.String("Query"),
			Method: []*descriptorpb.MethodDescriptorProto{{
				Name: proto.String("Lookup"), InputType: proto.String(".google.protobuf.StringValue"),
				OutputType: proto.String(".google.protobuf.StringValue"), Options: options,
			}},
		}},
	}, protoregistry.GlobalFiles)
	if err != nil {
		t.Fatal(err)
	}
	return Method{
		Name: "Lookup", Descriptor: file.Services().Get(0).Methods().Get(0),
		NewRequest: func() proto.Message { return new(wrapperspb.StringValue) },
		Invoke:     func(_ context.Context, input proto.Message) (proto.Message, error) { return input, nil },
	}
}
