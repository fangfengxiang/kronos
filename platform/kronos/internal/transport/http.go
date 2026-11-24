package transport

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/fangfengxiang/kronos/framework/app"
	kratoserrors "github.com/go-kratos/kratos/v3/errors"
	kratoshttp "github.com/go-kratos/kratos/v3/transport/http"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const (
	maxUnaryHTTPBody = 1 << 20
	unaryHTTPTimeout = 30 * time.Second
)

func UnaryHTTPServer(listener net.Listener, services []Service) (app.Server, error) {
	server := kratoshttp.NewServer(kratoshttp.Listener(listener), kratoshttp.Timeout(unaryHTTPTimeout),
		kratoshttp.ErrorEncoder(encodeUnaryHTTPError))
	router := server.Route("/")
	routes := make(map[string]string)
	for _, service := range services {
		for _, method := range service.Methods {
			options := method.Descriptor.Options()
			if !proto.HasExtension(options, annotations.E_Http) {
				continue
			}
			rule := proto.GetExtension(options, annotations.E_Http).(*annotations.HttpRule)
			verb, route, err := unaryHTTPRoute(rule)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", method.Descriptor.FullName(), err)
			}
			operation := "/" + service.Name + "/" + method.Name
			key := verb + " " + route
			if existing, duplicate := routes[key]; duplicate {
				return nil, fmt.Errorf("HTTP route %s is shared by %s and %s", key, existing, operation)
			}
			routes[key] = operation
			router.Handle(verb, route, unaryHTTPHandler(verb, operation, method))
		}
	}
	if len(routes) == 0 {
		return nil, errors.New("Kronos HTTP requires at least one annotated unary method")
	}
	return server, nil
}

func unaryHTTPRoute(rule *annotations.HttpRule) (verb, route string, err error) {
	switch {
	case rule.GetGet() != "" && rule.GetBody() == "":
		verb, route = http.MethodGet, rule.GetGet()
	case rule.GetPost() != "" && rule.GetBody() == "*":
		verb, route = http.MethodPost, rule.GetPost()
	default:
		return "", "", errors.New("HTTP requires GET without a body or POST with body \"*\"")
	}
	if rule.GetSelector() != "" || rule.GetResponseBody() != "" || len(rule.GetAdditionalBindings()) != 0 {
		return "", "", errors.New("HTTP requires a single complete response mapping")
	}
	if _, err := url.ParseRequestURI(route); err != nil || !strings.HasPrefix(route, "/") ||
		path.Clean(route) != route || strings.ContainsAny(route, "{}*%?#\\ \r\n\t") {
		return "", "", fmt.Errorf("HTTP requires a static path, received %q", route)
	}
	return verb, route, nil
}

func unaryHTTPHandler(verb, operation string, method Method) kratoshttp.HandlerFunc {
	return func(ctx kratoshttp.Context) error {
		request := method.NewRequest()
		switch verb {
		case http.MethodGet:
			if err := ctx.BindQuery(request); err != nil {
				return kratoserrors.BadRequest("INVALID_REQUEST", "invalid HTTP query")
			}
		case http.MethodPost:
			incoming := ctx.Request()
			mediaType, _, err := mime.ParseMediaType(incoming.Header.Get("Content-Type"))
			if err != nil || mediaType != "application/json" {
				return kratoserrors.New(http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "HTTP request requires application/json")
			}
			body, err := io.ReadAll(http.MaxBytesReader(ctx.Response(), incoming.Body, maxUnaryHTTPBody))
			if err != nil || len(body) != 0 && protojson.Unmarshal(body, request) != nil {
				return kratoserrors.BadRequest("INVALID_REQUEST", "invalid HTTP request body")
			}
		}
		kratoshttp.SetOperation(ctx, operation)
		response, err := method.Invoke(withIncomingRequest(ctx), request)
		if err != nil {
			code, reason, message := failure(err)
			return kratoserrors.New(code, reason, message)
		}
		body, err := protojson.Marshal(response)
		if err != nil {
			return kratoserrors.InternalServer("INTERNAL", "RPC failed")
		}
		return ctx.Blob(http.StatusOK, "application/json", body)
	}
}

func encodeUnaryHTTPError(response http.ResponseWriter, request *http.Request, err error) {
	jsonRequest := request.Clone(request.Context())
	jsonRequest.Header.Set("Accept", "application/json")
	kratoshttp.DefaultErrorEncoder(response, jsonRequest, err)
}
