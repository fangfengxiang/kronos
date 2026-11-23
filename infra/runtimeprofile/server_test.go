package runtimeprofile_test

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	standardpprof "net/http/pprof"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/infra/runtimeprofile"
)

func TestProfilesMatchGoHTTPHandlers(t *testing.T) {
	endpoint := startServer(t)
	for _, testCase := range []struct {
		profile    string
		sampleType string
	}{
		{profile: "heap", sampleType: "inuse_space/bytes"},
		{profile: "goroutine", sampleType: "goroutine/count"},
	} {
		t.Run(testCase.profile, func(t *testing.T) {
			path := "/debug/pprof/" + testCase.profile
			response := requestProfile(t, http.MethodGet, endpoint+path+"?debug=2")
			if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "application/octet-stream" || response.Header.Get("Cache-Control") != "no-store" {
				t.Fatalf("profile response = %s, headers %v", response.Status, response.Header)
			}
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			standardResponse := httptest.NewRecorder()
			standardpprof.Handler(testCase.profile).ServeHTTP(standardResponse, httptest.NewRequest(http.MethodGet, path, nil))
			if standardResponse.Code != http.StatusOK {
				t.Fatalf("standard profile response = %d", standardResponse.Code)
			}
			assertProfileSampleType(t, "kronos", body, testCase.sampleType)
			assertProfileSampleType(t, "go", standardResponse.Body.Bytes(), testCase.sampleType)
		})
	}
}

func TestProfileRoutesAreIsolated(t *testing.T) {
	endpoint := startServer(t)
	for _, path := range []string{"/", "/readyz", "/api/v1/run", "/debug/pprof/", "/debug/pprof/cmdline", "/debug/pprof/profile", "/debug/pprof/heap/"} {
		response := requestProfile(t, http.MethodGet, endpoint+path)
		if response.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s = %s, want 404", path, response.Status)
		}
	}
	for _, method := range []string{http.MethodPost, http.MethodHead} {
		response := requestProfile(t, method, endpoint+"/debug/pprof/heap")
		if response.StatusCode != http.StatusMethodNotAllowed || response.Header.Get("Allow") != http.MethodGet {
			t.Errorf("%s heap = %s, allow %q", method, response.Status, response.Header.Get("Allow"))
		}
	}
}

func TestProfileListenerRequiresLoopbackTCP(t *testing.T) {
	for _, address := range []net.Addr{
		&net.TCPAddr{IP: net.IPv4zero},
		&net.TCPAddr{IP: net.IPv6zero},
		&net.TCPAddr{IP: net.IPv4(192, 0, 2, 10)},
		&net.UnixAddr{Name: "profile.socket", Net: "unix"},
	} {
		if server, err := runtimeprofile.NewServer(addressedListener{address: address}); server != nil || err == nil {
			t.Errorf("NewServer(%v) = %v, %v", address, server, err)
		}
	}
}

func TestProfileServesIPv6Loopback(t *testing.T) {
	listener, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback unavailable: %v", err)
	}
	endpoint := serveProfiles(t, listener)
	response := requestProfile(t, http.MethodGet, endpoint+"/debug/pprof/goroutine")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("IPv6 profile = %s", response.Status)
	}
}

type addressedListener struct {
	net.Listener
	address net.Addr
}

func (listener addressedListener) Addr() net.Addr { return listener.address }

func startServer(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return serveProfiles(t, listener)
}

func serveProfiles(t *testing.T, listener net.Listener) string {
	t.Helper()
	t.Cleanup(func() { listener.Close() })
	server, err := runtimeprofile.NewServer(listener)
	if err != nil {
		t.Fatal(err)
	}
	stopped := make(chan error, 1)
	go func() { stopped <- server.Start(context.WithoutCancel(t.Context())) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := server.Stop(ctx); err != nil {
			t.Error(err)
		}
		select {
		case err := <-stopped:
			if err != nil {
				t.Error(err)
			}
		case <-ctx.Done():
			t.Error("profile server did not stop")
		}
	})
	return "http://" + listener.Addr().String()
}

func requestProfile(t *testing.T, method, url string) *http.Response {
	t.Helper()
	client := http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	request, err := http.NewRequestWithContext(t.Context(), method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { response.Body.Close() })
	return response
}

func assertProfileSampleType(t *testing.T, source string, profile []byte, sampleType string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), source+".pb.gz")
	if err := os.WriteFile(path, profile, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "go", "tool", "pprof", "-raw", path).CombinedOutput()
	if err != nil || !bytes.Contains(output, []byte(sampleType)) {
		t.Fatalf("%s profile does not contain %s: %v\n%s", source, sampleType, err, output)
	}
}
