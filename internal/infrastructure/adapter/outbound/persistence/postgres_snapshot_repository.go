package persistence

import (
	"context"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type PostgresSnapshotRepository struct {
	db *DB
}

func NewPostgresSnapshotRepository(db *DB) *PostgresSnapshotRepository {
	return &PostgresSnapshotRepository{db: db}
}

var _ outbound.SnapshotRepository = (*PostgresSnapshotRepository)(nil)

func (r *PostgresSnapshotRepository) FindAll(
	ctx context.Context,
	userId model.UserId,
) ([]model.NetWorthSnapshot, error) {
	query := `SELECT id, user_id, captured_at, total_value_usd FROM net_worth_snapshots
              WHERE user_id = $1 ORDER BY captured_at ASC`

	rows, err := r.db.Pool.Query(ctx, query, userId.UUID())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snapshots []model.NetWorthSnapshot
	for rows.Next() {
		s, err := mapRowToSnapshot(rows)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, s)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	return snapshots, nil
}

func (r *PostgresSnapshotRepository) Save(
	ctx context.Context,
	snapshot model.NetWorthSnapshot,
) (model.NetWorthSnapshot, error) {
	query := `INSERT INTO net_worth_snapshots (id, user_id, captured_at, total_value_usd) VALUES ($1, $2, $3, $4)`
	_, err := r.db.Pool.Exec(ctx, query, snapshot.Id.UUID(), snapshot.UserId.UUID(), snapshot.CapturedAt, snapshot.TotalValue.Amount())
	if err != nil {
		return model.NetWorthSnapshot{}, err
	}
	return snapshot, nil
}

func (r *PostgresSnapshotRepository) ExistsAt(
	ctx context.Context,
	userId model.UserId,
	capturedAt time.Time,
) (bool, error) {
	query := `SELECT count(*) FROM net_worth_snapshots WHERE user_id = $1 AND captured_at = $2`
	var count int
	err := r.db.Pool.QueryRow(ctx, query, userId.UUID(), capturedAt).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *PostgresSnapshotRepository) FindFirstOfYear(
	ctx context.Context,
	userId model.UserId,
	year int,
) (*model.NetWorthSnapshot, error) {
	yearStart := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	query := `SELECT id, user_id, captured_at, total_value_usd FROM net_worth_snapshots 
              WHERE user_id = $1 AND captured_at >= $2 ORDER BY captured_at ASC LIMIT 1`

	rows, err := r.db.Pool.Query(ctx, query, userId.UUID(), yearStart)
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

	s, err := mapRowToSnapshot(rows)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *PostgresSnapshotRepository) FindEarliest(
	ctx context.Context,
	userId model.UserId,
) (*model.NetWorthSnapshot, error) {
	query := `SELECT id, user_id, captured_at, total_value_usd FROM net_worth_snapshots 
              WHERE user_id = $1 ORDER BY captured_at ASC LIMIT 1`

	rows, err := r.db.Pool.Query(ctx, query, userId.UUID())
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

	s, err := mapRowToSnapshot(rows)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func mapRowToSnapshot(rows pgx.Rows) (model.NetWorthSnapshot, error) {
	var (
		id         uuid.UUID
		userId     uuid.UUID
		capturedAt time.Time
		totalValue decimal.Decimal
	)

	err := rows.Scan(&id, &userId, &capturedAt, &totalValue)
	if err != nil {
		return model.NetWorthSnapshot{}, err
	}

	m, err := model.NewMoney(totalValue)
	if err != nil {
		return model.NetWorthSnapshot{}, err
	}

	return model.NetWorthSnapshot{
		Id:         model.SnapshotIdFromUUID(id),
		UserId:     model.NewUserId(userId),
		CapturedAt: capturedAt,
		TotalValue: m,
	}, nil
}
