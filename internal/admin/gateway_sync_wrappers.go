package admin

import (
	"context"

	"github.com/brevyn/brevyn-cloud/internal/gateway/gatewayerror"
	"github.com/brevyn/brevyn-cloud/internal/gateway/sub2api"
)

func (h *Handler) defaultExternalGroupID(ctx context.Context) int64 {
	return h.redeem.DefaultExternalGroupID(ctx)
}

func (h *Handler) ensureAdminSub2APIAccount(ctx context.Context, client *sub2api.Client, user adminGatewayUser, defaultGroupID int64) (adminGatewayAccountSummary, error) {
	return h.redeem.EnsureSub2APIAccount(ctx, client, user, defaultGroupID)
}

func (h *Handler) updateAdminRedemptionStatus(ctx context.Context, redemptionDBID int64, status string, errInfo gatewayerror.Info, externalUserID, externalGroupID int64, operation string) error {
	return h.redeem.UpdateRedemptionStatus(ctx, redemptionDBID, status, errInfo, externalUserID, externalGroupID, operation)
}

func (h *Handler) updateAdminRedemptionError(ctx context.Context, redemptionDBID int64, errInfo gatewayerror.Info) error {
	return h.redeem.UpdateRedemptionError(ctx, redemptionDBID, errInfo)
}
