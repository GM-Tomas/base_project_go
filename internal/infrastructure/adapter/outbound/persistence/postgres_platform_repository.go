package persistence

import (
	"context"
	"fmt"
	"strings"
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

func (r *PostgresPlatformRepository) FindByName(
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
	existing, err := r.FindByName(ctx, userId, name)
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
		existingRetry, errRetry := r.FindByName(ctx, userId, name)
		if errRetry == nil && existingRetry != nil {
			return existingRetry.Name, nil
		}
		return model.PlatformName{}, err
	}

	return name, nil
}

func (r *PostgresPlatformRepository) Save(
	ctx context.Context,
	platform model.Platform,
) (model.Platform, error) {
	query := `
        INSERT INTO platforms (user_id, name, type, created_at) 
        VALUES ($1, $2, $3, $4)
        ON CONFLICT (user_id, name) DO UPDATE SET type = EXCLUDED.type
    `
	_, err := r.db.Pool.Exec(ctx, query, platform.UserId.UUID(), platform.Name.Value(), platform.Type.Value(), platform.CreatedAt)
	if err != nil {
		return model.Platform{}, err
	}
	return platform, nil
}

func (r *PostgresPlatformRepository) Update(
	ctx context.Context,
	userId model.UserId,
	currentName model.PlatformName,
	newName *model.PlatformName,
	newType *model.PlatformType,
) (*model.Platform, error) {
	if newName == nil && newType == nil {
		return r.FindByName(ctx, userId, currentName)
	}

	sets := []string{}
	args := []any{userId.UUID(), currentName.Value()}
	argIdx := 3

	if newName != nil {
		sets = append(sets, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, newName.Value())
		argIdx++
	}
	if newType != nil {
		sets = append(sets, fmt.Sprintf("type = $%d", argIdx))
		args = append(args, newType.Value())
		argIdx++
	}

	query := fmt.Sprintf("UPDATE platforms SET %s WHERE user_id = $1 AND lower(name) = lower($2)", strings.Join(sets, ", "))
	tag, err := r.db.Pool.Exec(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, nil
	}

	targetName := currentName
	if newName != nil {
		targetName = *newName
	}
	return r.FindByName(ctx, userId, targetName)
}

func (r *PostgresPlatformRepository) DeleteByName(
	ctx context.Context,
	userId model.UserId,
	name model.PlatformName,
) (bool, error) {
	query := `DELETE FROM platforms WHERE user_id = $1 AND lower(name) = lower($2)`
	tag, err := r.db.Pool.Exec(ctx, query, userId.UUID(), name.Value())
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *PostgresPlatformRepository) CountHoldings(
	ctx context.Context,
	userId model.UserId,
	name model.PlatformName,
) (int, error) {
	query := `SELECT count(*) FROM holdings WHERE user_id = $1 AND platform_name = $2`
	var count int
	err := r.db.Pool.QueryRow(ctx, query, userId.UUID(), name.Value()).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
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
