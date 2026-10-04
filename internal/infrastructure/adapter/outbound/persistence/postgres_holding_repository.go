package persistence

import (
	"context"
	"errors"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type PostgresHoldingRepository struct {
	db *DB
}

func NewPostgresHoldingRepository(db *DB) *PostgresHoldingRepository {
	return &PostgresHoldingRepository{db: db}
}

var _ outbound.HoldingRepository = (*PostgresHoldingRepository)(nil)

func (r *PostgresHoldingRepository) FindAll(
	ctx context.Context,
	userId model.UserId,
) ([]model.Holding, error) {
	query := `SELECT id, user_id, name, asset_class, platform_name, value_usd, created_at, updated_at
              FROM holdings WHERE user_id = $1 ORDER BY created_at ASC`

	rows, err := r.db.Pool.Query(ctx, query, userId.UUID())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var holdings []model.Holding
	for rows.Next() {
		h, err := mapRowToHolding(rows)
		if err != nil {
			return nil, err
		}
		holdings = append(holdings, h)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	return holdings, nil
}

func (r *PostgresHoldingRepository) Save(
	ctx context.Context,
	holding model.Holding,
) (model.Holding, error) {
	query := `
        INSERT INTO holdings (id, user_id, name, asset_class, platform_name, value_usd, created_at, updated_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
    `

	_, err := r.db.Pool.Exec(
		ctx,
		query,
		holding.Id.UUID(),
		holding.UserId.UUID(),
		holding.Name,
		holding.AssetClass.Value(),
		holding.Platform.Value(),
		holding.Value.Amount(),
		holding.CreatedAt,
		holding.UpdatedAt,
	)
	if err != nil {
		return model.Holding{}, err
	}

	return holding, nil
}

func (r *PostgresHoldingRepository) DeleteById(
	ctx context.Context,
	userId model.UserId,
	id model.HoldingId,
) (bool, error) {
	query := `DELETE FROM holdings WHERE id = $1 AND user_id = $2`
	tag, err := r.db.Pool.Exec(ctx, query, id.UUID(), userId.UUID())
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *PostgresHoldingRepository) AssetClassesInUse(
	ctx context.Context,
	userId model.UserId,
) ([]model.AssetClass, error) {
	query := `SELECT DISTINCT asset_class FROM holdings WHERE user_id = $1 ORDER BY asset_class`
	rows, err := r.db.Pool.Query(ctx, query, userId.UUID())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var classes []model.AssetClass
	for rows.Next() {
		var className string
		if err := rows.Scan(&className); err != nil {
			return nil, err
		}
		ac, err := model.NewAssetClass(className)
		if err != nil {
			return nil, err
		}
		classes = append(classes, ac)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	return classes, nil
}

func mapRowToHolding(rows pgx.Rows) (model.Holding, error) {
	var (
		id          uuid.UUID
		userId      uuid.UUID
		name        string
		assetClass  string
		platform    string
		valueAmount decimal.Decimal
		createdAt   time.Time
		updatedAt   time.Time
	)

	err := rows.Scan(&id, &userId, &name, &assetClass, &platform, &valueAmount, &createdAt, &updatedAt)
	if err != nil {
		return model.Holding{}, err
	}

	ac, err := model.NewAssetClass(assetClass)
	if err != nil {
		return model.Holding{}, err
	}

	pn, err := model.NewPlatformName(platform)
	if err != nil {
		return model.Holding{}, err
	}

	m, err := model.NewMoney(valueAmount)
	if err != nil {
		return model.Holding{}, err
	}

	if name == "" {
		return model.Holding{}, errors.New("empty holding name from db")
	}

	return model.Holding{
		Id:         model.HoldingIdFromUUID(id),
		UserId:     model.NewUserId(userId),
		Name:       name,
		AssetClass: ac,
		Platform:   pn,
		Value:      m,
		CreatedAt:  createdAt,
		UpdatedAt:  updatedAt,
	}, nil
}
