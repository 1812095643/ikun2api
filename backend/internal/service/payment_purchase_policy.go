package service

import (
	"context"
	"github.com/MACOS-DO/sub4api/internal/payment"
	infraerrors "github.com/MACOS-DO/sub4api/internal/pkg/errors"
)

func validateUserSubscriptionPurchase(user *User, orderType string) error {
	if orderType == payment.OrderTypeSubscription && user != nil && user.IsDownstream && user.DownstreamRechargeOnly {
		return infraerrors.Forbidden("SUBSCRIPTION_PURCHASE_DISABLED", "当前账号仅支持余额充值")
	}
	return nil
}

// SubscriptionPurchaseAllowed 只限制下游的新订阅购买，保留余额充值与已有订阅的使用。
func (s *PaymentService) SubscriptionPurchaseAllowed(ctx context.Context, userID int64) (bool, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return false, err
	}
	return validateUserSubscriptionPurchase(user, payment.OrderTypeSubscription) == nil, nil
}
