package admin

import (
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

const invalidTextInputError = "invalid_text_input"

func sanitizeDatabaseText(value string) string {
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r == 0:
			return -1
		case r == '\n' || r == '\r' || r == '\t':
			return r
		case r < 0x20 || r == 0x7f:
			return -1
		default:
			return r
		}
	}, value)
	return strings.TrimSpace(cleaned)
}

func sanitizeProductRequest(req *createProductRequest) {
	req.SKU = sanitizeDatabaseText(req.SKU)
	req.Name = sanitizeDatabaseText(req.Name)
	req.Description = sanitizeDatabaseText(req.Description)
	req.BenefitType = sanitizeDatabaseText(req.BenefitType)
	req.GatewayGroupID = sanitizeDatabaseText(req.GatewayGroupID)
	req.Source = sanitizeDatabaseText(req.Source)
	req.Features = sanitizeDatabaseText(req.Features)
	req.Status = sanitizeDatabaseText(req.Status)
	req.AuditReason = sanitizeDatabaseText(req.AuditReason)
	req.Reason = sanitizeDatabaseText(req.Reason)
}

func sanitizeGenerateRedeemCodesRequest(req *generateRedeemCodesRequest) {
	req.ProductID = sanitizeDatabaseText(req.ProductID)
	req.BatchName = sanitizeDatabaseText(req.BatchName)
	req.Source = sanitizeDatabaseText(req.Source)
	req.OrderRef = sanitizeDatabaseText(req.OrderRef)
	req.Notes = sanitizeDatabaseText(req.Notes)
	req.IdempotencyKey = sanitizeDatabaseText(req.IdempotencyKey)
	req.AuditReason = sanitizeDatabaseText(req.AuditReason)
	req.Reason = sanitizeDatabaseText(req.Reason)
}

func isPostgresInvalidTextError(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "22021" {
		return true
	}
	return strings.Contains(err.Error(), "invalid byte sequence for encoding")
}
