package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type CatalogService struct {
	postgres *pgxpool.Pool
}

func NewCatalogService(postgres *pgxpool.Pool) *CatalogService {
	return &CatalogService{postgres: postgres}
}

func (s *CatalogService) ListProducts(ctx context.Context) ([]ProductItem, error) {
	rows, err := s.postgres.Query(ctx, `
		SELECT p.public_id, p.sku, p.name, p.description, p.benefit_type, p.price_cny,
			p.original_price_cny, p.value, p.validity_days, coalesce(gg.public_id, ''),
			coalesce(gg.name, ''), p.external_group_id, p.source, p.features, p.for_sale,
			p.sort_order, p.status, p.created_at, p.updated_at
		FROM products p
		LEFT JOIN gateway_groups gg ON gg.id = p.gateway_group_id
		ORDER BY p.sort_order ASC, p.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []ProductItem{}
	for rows.Next() {
		item, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *CatalogService) CreateProduct(ctx context.Context, req createProductRequest) (ProductItem, error) {
	if err := validateProductRequest(&req, true); err != nil {
		return ProductItem{}, err
	}
	gatewayGroupDBID, externalGroupID, err := s.resolveGatewayGroup(ctx, req.GatewayGroupID, req.ExternalGroupID)
	if err != nil {
		return ProductItem{}, fmt.Errorf("gateway_group_not_found")
	}
	if err := s.validateProductGatewayGroup(ctx, req.BenefitType, gatewayGroupDBID); err != nil {
		return ProductItem{}, err
	}
	if externalGroupID != 0 {
		req.ExternalGroupID = externalGroupID
	}
	forSale := true
	if req.ForSale != nil {
		forSale = *req.ForSale
	}
	if req.Source == "" {
		req.Source = "ldxp"
	}
	if req.Status == "" {
		req.Status = "active"
	}

	row := s.postgres.QueryRow(ctx, `
		WITH inserted AS (
			INSERT INTO products (
				public_id, sku, name, description, benefit_type, price_cny, original_price_cny,
				value, validity_days, gateway_group_id, external_group_id, source, features,
				for_sale, sort_order, status
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
			RETURNING *
		)
		SELECT i.public_id, i.sku, i.name, i.description, i.benefit_type, i.price_cny,
			i.original_price_cny, i.value, i.validity_days, coalesce(gg.public_id, ''),
			coalesce(gg.name, ''), i.external_group_id, i.source, i.features, i.for_sale,
			i.sort_order, i.status, i.created_at, i.updated_at
		FROM inserted i
		LEFT JOIN gateway_groups gg ON gg.id = i.gateway_group_id
	`, "prd_"+uuid.NewString(), req.SKU, req.Name, req.Description, req.BenefitType,
		req.PriceCNY, req.OriginalPriceCNY, req.Value, req.ValidityDays, gatewayGroupDBID,
		req.ExternalGroupID, req.Source, req.Features, forSale, req.SortOrder, req.Status)
	return scanProduct(row)
}

func (s *CatalogService) UpdateProduct(ctx context.Context, productID string, req createProductRequest) (ProductItem, error) {
	if err := validateProductRequest(&req, false); err != nil {
		return ProductItem{}, err
	}
	gatewayGroupDBID, externalGroupID, err := s.resolveGatewayGroup(ctx, req.GatewayGroupID, req.ExternalGroupID)
	if err != nil {
		return ProductItem{}, fmt.Errorf("gateway_group_not_found")
	}
	if err := s.validateProductGatewayGroup(ctx, req.BenefitType, gatewayGroupDBID); err != nil {
		return ProductItem{}, err
	}
	if externalGroupID != 0 {
		req.ExternalGroupID = externalGroupID
	}
	forSale := true
	if req.ForSale != nil {
		forSale = *req.ForSale
	}
	if req.Source == "" {
		req.Source = "ldxp"
	}
	if req.Status == "" {
		req.Status = "active"
	}

	row := s.postgres.QueryRow(ctx, `
		WITH updated AS (
			UPDATE products
			SET sku = $2,
				name = $3,
				description = $4,
				benefit_type = $5,
				price_cny = $6,
				original_price_cny = $7,
				value = $8,
				validity_days = $9,
				gateway_group_id = $10,
				external_group_id = $11,
				source = $12,
				features = $13,
				for_sale = $14,
				sort_order = $15,
				status = $16,
				updated_at = now()
			WHERE public_id = $1
			RETURNING *
		)
		SELECT u.public_id, u.sku, u.name, u.description, u.benefit_type, u.price_cny,
			u.original_price_cny, u.value, u.validity_days, coalesce(gg.public_id, ''),
			coalesce(gg.name, ''), u.external_group_id, u.source, u.features, u.for_sale,
			u.sort_order, u.status, u.created_at, u.updated_at
		FROM updated u
		LEFT JOIN gateway_groups gg ON gg.id = u.gateway_group_id
	`, productID, req.SKU, req.Name, req.Description, req.BenefitType,
		req.PriceCNY, req.OriginalPriceCNY, req.Value, req.ValidityDays, gatewayGroupDBID,
		req.ExternalGroupID, req.Source, req.Features, forSale, req.SortOrder, req.Status)
	return scanProduct(row)
}

func (s *CatalogService) ArchiveProduct(ctx context.Context, productID string) (ProductItem, error) {
	var dbID int64
	if err := s.postgres.QueryRow(ctx, `
		SELECT id FROM products WHERE public_id = $1
	`, productID).Scan(&dbID); err != nil {
		return ProductItem{}, err
	}

	row := s.postgres.QueryRow(ctx, `
		WITH updated AS (
			UPDATE products
			SET status = 'archived',
				for_sale = false,
				updated_at = now()
			WHERE id = $1
			RETURNING *
		)
		SELECT u.public_id, u.sku, u.name, u.description, u.benefit_type, u.price_cny,
			u.original_price_cny, u.value, u.validity_days, coalesce(gg.public_id, ''),
			coalesce(gg.name, ''), u.external_group_id, u.source, u.features, u.for_sale,
			u.sort_order, u.status, u.created_at, u.updated_at
		FROM updated u
		LEFT JOIN gateway_groups gg ON gg.id = u.gateway_group_id
	`, dbID)
	return scanProduct(row)
}

func (s *CatalogService) GenerateRedeemCodes(ctx context.Context, req generateRedeemCodesRequest, adminID int64, expiresAt *time.Time) (map[string]any, error) {
	tx, err := s.postgres.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	product, err := loadProductForGeneration(ctx, tx, req.ProductID)
	if err != nil {
		return nil, err
	}
	if err := validateProductForGeneration(product); err != nil {
		return nil, err
	}

	var batchDBID int64
	var batchPublicID string
	err = tx.QueryRow(ctx, `
		INSERT INTO redeem_code_batches (public_id, product_id, name, source, order_ref, quantity, notes, created_by_admin_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, public_id
	`, "rb_"+uuid.NewString(), product.dbID, req.BatchName, req.Source, req.OrderRef, req.Count, req.Notes, nullableAdminID(adminID)).Scan(&batchDBID, &batchPublicID)
	if err != nil {
		return nil, err
	}

	generated := make([]generatedRedeemCode, 0, req.Count)
	for i := 0; i < req.Count; i++ {
		code, err := generateCodeValue()
		if err != nil {
			return nil, err
		}
		prefix := codePrefix(code)
		_, err = tx.Exec(ctx, `
			INSERT INTO redeem_codes (
				public_id, code_hash, code_prefix, kind, value, status, expires_at, order_ref, notes,
				product_id, batch_id, gateway_group_id, external_group_id, validity_days, source
			)
			VALUES ($1, $2, $3, $4, $5, 'unused', $6, $7, $8, $9, $10, $11, $12, $13, $14)
		`, "rc_"+uuid.NewString(), hashCode(code), prefix, product.BenefitType, product.Value,
			expiresAt, req.OrderRef, req.Notes, product.dbID, batchDBID, product.gatewayGroupDBID,
			product.ExternalGroupID, product.ValidityDays, req.Source)
		if err != nil {
			return nil, err
		}
		generated = append(generated, generatedRedeemCode{
			Code:       code,
			MaskedCode: maskCode(prefix),
			CodePrefix: prefix,
		})
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return map[string]any{
		"batch": map[string]any{
			"id":       batchPublicID,
			"name":     req.BatchName,
			"quantity": req.Count,
			"source":   req.Source,
			"orderRef": req.OrderRef,
			"notes":    req.Notes,
		},
		"product": product,
		"codes":   generated,
	}, nil
}

func (s *CatalogService) DisableRedeemCode(ctx context.Context, codeID string) (RedeemCodeItem, error) {
	codeID = strings.TrimSpace(codeID)
	tag, err := s.postgres.Exec(ctx, `
		UPDATE redeem_codes
		SET status = 'disabled', updated_at = now()
		WHERE public_id = $1 AND status = 'unused'
	`, codeID)
	if err != nil {
		return RedeemCodeItem{}, err
	}
	if tag.RowsAffected() == 0 {
		var status string
		if err := s.postgres.QueryRow(ctx, `
			SELECT status FROM redeem_codes WHERE public_id = $1
		`, codeID).Scan(&status); err != nil {
			return RedeemCodeItem{}, err
		}
		return RedeemCodeItem{}, fmt.Errorf("redeem_code_not_unused")
	}
	return s.queryRedeemCodeByID(ctx, codeID)
}

func (s *CatalogService) DisableRedeemBatch(ctx context.Context, batchID string) (RedeemBatchItem, int, error) {
	batchID = strings.TrimSpace(batchID)
	var batchDBID int64
	if err := s.postgres.QueryRow(ctx, `
		SELECT id FROM redeem_code_batches WHERE public_id = $1
	`, batchID).Scan(&batchDBID); err != nil {
		return RedeemBatchItem{}, 0, err
	}
	tag, err := s.postgres.Exec(ctx, `
		UPDATE redeem_codes
		SET status = 'disabled', updated_at = now()
		WHERE batch_id = $1 AND status = 'unused'
	`, batchDBID)
	if err != nil {
		return RedeemBatchItem{}, 0, err
	}
	_, err = s.postgres.Exec(ctx, `
		UPDATE redeem_code_batches
		SET status = 'disabled', updated_at = now()
		WHERE id = $1 AND status = 'active'
	`, batchDBID)
	if err != nil {
		return RedeemBatchItem{}, 0, err
	}
	item, err := s.queryRedeemBatchByID(ctx, batchID)
	return item, int(tag.RowsAffected()), err
}

func (s *CatalogService) queryRedeemBatchByID(ctx context.Context, batchID string) (RedeemBatchItem, error) {
	var item RedeemBatchItem
	err := s.postgres.QueryRow(ctx, `
		SELECT b.public_id, b.name, b.source, b.order_ref, b.quantity, b.status, b.notes,
			coalesce(p.public_id, ''), coalesce(p.name, ''),
			count(rc.id) FILTER (WHERE rc.status = 'unused')::int AS unused_count,
			count(rc.id) FILTER (WHERE rc.status = 'used')::int AS used_count,
			b.created_at
		FROM redeem_code_batches b
		LEFT JOIN products p ON p.id = b.product_id
		LEFT JOIN redeem_codes rc ON rc.batch_id = b.id
		WHERE b.public_id = $1
		GROUP BY b.id, p.public_id, p.name
	`, batchID).Scan(&item.ID, &item.Name, &item.Source, &item.OrderRef, &item.Quantity, &item.Status,
		&item.Notes, &item.ProductID, &item.ProductName, &item.UnusedCount, &item.UsedCount,
		&item.CreatedAt)
	return item, err
}

func (s *CatalogService) queryRedeemCodeByID(ctx context.Context, codeID string) (RedeemCodeItem, error) {
	return scanRedeemCode(s.postgres.QueryRow(ctx, `
		SELECT rc.public_id, rc.code_prefix, rc.kind, rc.value, rc.validity_days, rc.status, rc.order_ref, rc.notes,
			coalesce(p.public_id, ''), coalesce(p.name, ''), coalesce(p.sku, ''),
			coalesce(b.public_id, ''), coalesce(b.name, ''),
			rc.external_group_id, rc.source, coalesce(u.public_id, ''), coalesce(u.email, ''),
			rc.used_at, rc.expires_at, rc.created_at
		FROM redeem_codes rc
		LEFT JOIN products p ON p.id = rc.product_id
		LEFT JOIN redeem_code_batches b ON b.id = rc.batch_id
		LEFT JOIN users u ON u.id = rc.used_by_user_id
		WHERE rc.public_id = $1
	`, codeID))
}

func (s *CatalogService) validateProductGatewayGroup(ctx context.Context, benefitType string, gatewayGroupDBID *int64) error {
	if gatewayGroupDBID == nil {
		if benefitType == "subscription" {
			return fmt.Errorf("subscription_product_requires_gateway_group")
		}
		return nil
	}

	var subscriptionType string
	var status string
	err := s.postgres.QueryRow(ctx, `
		SELECT subscription_type, status FROM gateway_groups WHERE id = $1
	`, *gatewayGroupDBID).Scan(&subscriptionType, &status)
	if err != nil {
		return err
	}
	if status != "active" {
		return fmt.Errorf("product_gateway_group_not_active")
	}
	if benefitType == "balance" && subscriptionType == "subscription" {
		return fmt.Errorf("balance_product_requires_standard_group")
	}
	if benefitType == "subscription" && subscriptionType != "subscription" {
		return fmt.Errorf("subscription_product_requires_subscription_group")
	}
	return nil
}

func (s *CatalogService) resolveGatewayGroup(ctx context.Context, publicID string, externalGroupID int64) (*int64, int64, error) {
	publicID = strings.TrimSpace(publicID)
	if publicID == "" && externalGroupID == 0 {
		return nil, 0, nil
	}
	var dbID int64
	var external int64
	var err error
	if publicID != "" {
		err = s.postgres.QueryRow(ctx, `
			SELECT id, external_group_id FROM gateway_groups WHERE public_id = $1
		`, publicID).Scan(&dbID, &external)
	} else {
		err = s.postgres.QueryRow(ctx, `
			SELECT id, external_group_id FROM gateway_groups WHERE provider = 'sub2api' AND external_group_id = $1
		`, externalGroupID).Scan(&dbID, &external)
	}
	if err != nil {
		return nil, 0, err
	}
	return &dbID, external, nil
}

func isProductValidationError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return false
	}
	switch err.Error() {
	case "sku_required",
		"name_required",
		"balance_value_required",
		"balance_product_requires_standard_group",
		"validity_days_required",
		"unsupported_benefit_type",
		"price_invalid",
		"gateway_group_not_found",
		"product_gateway_group_not_active",
		"subscription_product_requires_gateway_group",
		"subscription_product_requires_subscription_group":
		return true
	default:
		return false
	}
}
