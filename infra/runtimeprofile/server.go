// Package runtimeprofile serves on-demand Go profiles on a loopback listener.
package runtimeprofile

import (
	"context"
	"errors"
	"net"
	"net/http"
	"runtime/pprof"

	kratoshttp "github.com/go-kratos/kratos/v3/transport/http"
)

// Server serves heap and goroutine profiles. Construct it with NewServer.
type Server struct {
	server *kratoshttp.Server
}

// NewServer builds a profile server for a bound loopback TCP listener.
// The caller is responsible for closing the listener if construction fails.
func NewServer(listener net.Listener) (*Server, error) {
	var address *net.TCPAddr
	if listener != nil {
		address, _ = listener.Addr().(*net.TCPAddr)
	}
	if address == nil || !address.IP.IsLoopback() {
		return nil, errors.New("runtime profiles require a loopback TCP listener")
	}
	server := kratoshttp.NewServer(
		kratoshttp.Address(address.String()),
		kratoshttp.Listener(listener),
		kratoshttp.StrictSlash(false),
		kratoshttp.NotFoundHandler(http.NotFoundHandler()),
	)
	server.HandleFunc("/debug/pprof/heap", profileHandler(pprof.Lookup("heap")))
	server.HandleFunc("/debug/pprof/goroutine", profileHandler(pprof.Lookup("goroutine")))
	return &Server{server: server}, nil
}

// Start blocks while serving the listener.
func (server *Server) Start(ctx context.Context) error {
	return server.server.Start(ctx)
}

// Stop drains the server within the supplied deadline.
func (server *Server) Stop(ctx context.Context) error {
	return server.server.Stop(ctx)
}

func profileHandler(profile *pprof.Profile) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			response.Header().Set("Allow", http.MethodGet)
			http.Error(response, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		response.Header().Set("Content-Type", "application/octet-stream")
		response.Header().Set("Content-Disposition", `attachment; filename="`+profile.Name()+`.pb.gz"`)
		response.Header().Set("Cache-Control", "no-store")
		// WriteTo emits the binary profile directly to the client.
		_ = profile.WriteTo(response, 0)
	}
}
