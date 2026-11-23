package order

import (
	"context"
	"strings"

	"github.com/fangfengxiang/kronos/platform/kronos"
)

type orderService struct{}

// NewService constructs the reference Order implementation.
func NewService() OrderServiceUnaryService {
	return orderService{}
}

func (orderService) GetOrder(ctx context.Context, request *GetOrderRequest) (*GetOrderResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	orderID := strings.TrimSpace(request.GetOrderId())
	if orderID == "" {
		return nil, &kronos.Error{
			Code: kronos.InvalidArgument, Reason: ErrorReason_ERROR_REASON_INVALID_ORDER_ID.String(),
			Message: "order ID must not be blank",
		}
	}
	return &GetOrderResponse{OrderId: orderID, Status: "confirmed"}, nil
}
