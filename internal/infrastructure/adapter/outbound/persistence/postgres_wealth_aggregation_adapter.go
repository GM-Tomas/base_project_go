package persistence

import (
	"context"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	"github.com/shopspring/decimal"
)

type PostgresWealthAggregationAdapter struct {
	db *DB
}

func NewPostgresWealthAggregationAdapter(db *DB) *PostgresWealthAggregationAdapter {
	return &PostgresWealthAggregationAdapter{db: db}
}

var _ outbound.WealthAggregationPort = (*PostgresWealthAggregationAdapter)(nil)

func (a *PostgresWealthAggregationAdapter) NetWorth(
	ctx context.Context,
	userId model.UserId,
) (model.Money, error) {
	query := `SELECT COALESCE(SUM(value_usd), 0) FROM holdings WHERE user_id = $1`
	var total decimal.Decimal
	err := a.db.Pool.QueryRow(ctx, query, userId.UUID()).Scan(&total)
	if err != nil {
		return model.ZeroMoney, err
	}
	return model.NewMoney(total)
}

func (a *PostgresWealthAggregationAdapter) ByAssetClass(
	ctx context.Context,
	userId model.UserId,
) ([]outbound.AssetClassAggregate, error) {
	query := `
        SELECT asset_class, SUM(value_usd) AS total, COUNT(*) AS cnt 
        FROM holdings 
        WHERE user_id = $1 
        GROUP BY asset_class 
        ORDER BY total DESC
    `
	rows, err := a.db.Pool.Query(ctx, query, userId.UUID())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []outbound.AssetClassAggregate
	for rows.Next() {
		var (
			assetClass string
			total      decimal.Decimal
			count      int
		)
		if err := rows.Scan(&assetClass, &total, &count); err != nil {
			return nil, err
		}

		ac, err := model.NewAssetClass(assetClass)
		if err != nil {
			return nil, err
		}

		m, err := model.NewMoney(total)
		if err != nil {
			return nil, err
		}

		list = append(list, outbound.AssetClassAggregate{
			AssetClass: ac,
			Value:      m,
			Count:      count,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	return list, nil
}

func (a *PostgresWealthAggregationAdapter) ByPlatform(
	ctx context.Context,
	userId model.UserId,
) ([]outbound.PlatformAggregate, error) {
	query := `
        SELECT p.name, p.type, COALESCE(SUM(h.value_usd), 0) AS total, COUNT(h.id) AS cnt
        FROM platforms p
        LEFT JOIN holdings h ON h.user_id = p.user_id AND h.platform_name = p.name
        WHERE p.user_id = $1
        GROUP BY p.name, p.type
        ORDER BY total DESC, p.name
    `
	rows, err := a.db.Pool.Query(ctx, query, userId.UUID())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []outbound.PlatformAggregate
	for rows.Next() {
		var (
			name  string
			pType string
			total decimal.Decimal
			count int
		)
		if err := rows.Scan(&name, &pType, &total, &count); err != nil {
			return nil, err
		}

		pn, err := model.NewPlatformName(name)
		if err != nil {
			return nil, err
		}

		pt, err := model.NewPlatformType(pType)
		if err != nil {
			return nil, err
		}

		m, err := model.NewMoney(total)
		if err != nil {
			return nil, err
		}

		list = append(list, outbound.PlatformAggregate{
			Name:  pn,
			Type:  pt,
			Value: m,
			Count: count,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	return list, nil
}
