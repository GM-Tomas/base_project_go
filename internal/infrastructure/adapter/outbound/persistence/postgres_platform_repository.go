package persistence

import (
	"context"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type PostgresPlatformRepository struct {
	db *DB
}

func NewPostgresPlatformRepository(db *DB) *PostgresPlatformRepository {
	return &PostgresPlatformRepository{db: db}
}

var _ outbound.PlatformRepository = (*PostgresPlatformRepository)(nil)

func (r *PostgresPlatformRepository) FindAll(
	ctx context.Context,
	userId model.UserId,
) ([]model.Platform, error) {
	query := `SELECT user_id, name, type, created_at FROM platforms WHERE user_id = $1 ORDER BY name`
	rows, err := r.db.Pool.Query(ctx, query, userId.UUID())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var platforms []model.Platform
	for rows.Next() {
		p, err := mapRowToPlatform(rows)
		if err != nil {
			return nil, err
		}
		platforms = append(platforms, p)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	return platforms, nil
}

func (r *PostgresPlatformRepository) findByName(
	ctx context.Context,
	userId model.UserId,
	name model.PlatformName,
) (*model.Platform, error) {
	query := `SELECT user_id, name, type, created_at FROM platforms WHERE user_id = $1 AND lower(name) = lower($2)`
	rows, err := r.db.Pool.Query(ctx, query, userId.UUID(), name.Value())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, nil
	}

	p, err := mapRowToPlatform(rows)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *PostgresPlatformRepository) EnsureExists(
	ctx context.Context,
	userId model.UserId,
	name model.PlatformName,
	now time.Time,
) (model.PlatformName, error) {
	existing, err := r.findByName(ctx, userId, name)
	if err != nil {
		return model.PlatformName{}, err
	}
	if existing != nil {
		return existing.Name, nil
	}

	query := `INSERT INTO platforms (user_id, name, type, created_at) VALUES ($1, $2, $3, $4)`
	_, err = r.db.Pool.Exec(ctx, query, userId.UUID(), name.Value(), model.DefaultPlatformType, now)
	if err != nil {
		// If concurrent insert happened, retry find
		existingRetry, errRetry := r.findByName(ctx, userId, name)
		if errRetry == nil && existingRetry != nil {
			return existingRetry.Name, nil
		}
		return model.PlatformName{}, err
	}

	return name, nil
}

func (r *PostgresPlatformRepository) DeleteUnused(ctx context.Context, userId model.UserId) error {
	query := `DELETE FROM platforms p WHERE p.user_id = $1
              AND NOT EXISTS (SELECT 1 FROM holdings h WHERE h.user_id = p.user_id AND h.platform_name = p.name)`
	_, err := r.db.Pool.Exec(ctx, query, userId.UUID())
	return err
}

func mapRowToPlatform(rows pgx.Rows) (model.Platform, error) {
	var (
		userId    uuid.UUID
		name      string
		pType     string
		createdAt time.Time
	)

	err := rows.Scan(&userId, &name, &pType, &createdAt)
	if err != nil {
		return model.Platform{}, err
	}

	pn, err := model.NewPlatformName(name)
	if err != nil {
		return model.Platform{}, err
	}

	pt, err := model.NewPlatformType(pType)
	if err != nil {
		return model.Platform{}, err
	}

	return model.Platform{
		UserId:    model.NewUserId(userId),
		Name:      pn,
		Type:      pt,
		CreatedAt: createdAt,
	}, nil
}
