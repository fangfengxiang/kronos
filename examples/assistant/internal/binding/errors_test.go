package binding

import (
	"errors"
	"net/http"
	"testing"

	"github.com/fangfengxiang/kronos/framework/rpc"
	kratoserrors "github.com/go-kratos/kratos/v3/errors"
	"google.golang.org/grpc/codes"
)

func TestServiceUnavailableKeepsItsCodeAndReason(t *testing.T) {
	wireError := toWireError(&rpc.Error{
		Code:    rpc.Unavailable,
		Reason:  rpc.ServiceUnavailableReason,
		Message: "service is unavailable",
	})
	var httpFailure *kratoserrors.Error
	if !errors.As(wireError, &httpFailure) || httpFailure.Code != http.StatusServiceUnavailable {
		t.Fatalf("HTTP wire error = %v, want HTTP 503", wireError)
	}
	grpcFailure := httpFailure.GRPCStatus()
	if grpcFailure.Code() != codes.Unavailable {
		t.Fatalf("gRPC status = %v, want Unavailable", grpcFailure)
	}
	for _, err := range []error{httpFailure, grpcFailure.Err()} {
		decoded := fromWireError(err)
		var publicError *rpc.Error
		if !errors.As(decoded, &publicError) || publicError.Code != rpc.Unavailable || publicError.Reason != rpc.ServiceUnavailableReason {
			t.Fatalf("decoded wire error = %v, want service unavailable", decoded)
		}
	}
}
