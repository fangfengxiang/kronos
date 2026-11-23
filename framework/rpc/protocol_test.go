package rpc_test

import (
	"errors"
	"testing"

	"github.com/fangfengxiang/kronos/framework/rpc"
)

func TestResolveProtocol(t *testing.T) {
	if protocol, err := rpc.ResolveProtocol(); protocol != rpc.ProtocolGRPC || err != nil {
		t.Fatalf("default protocol = %q, %v", protocol, err)
	}
	if protocol, err := rpc.ResolveProtocol(rpc.WithProtocol(rpc.ProtocolGRPC), rpc.WithProtocol(rpc.ProtocolMicroV2)); protocol != rpc.ProtocolMicroV2 || err != nil {
		t.Fatalf("last protocol option = %q, %v", protocol, err)
	}
	if _, err := rpc.ResolveProtocol(rpc.WithProtocol("unknown")); !errors.Is(err, rpc.ErrUnsupportedProtocol) {
		t.Fatalf("unknown protocol error = %v", err)
	}
}
