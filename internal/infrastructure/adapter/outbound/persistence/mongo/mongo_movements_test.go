package mongo

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	appErrors "github.com/GM-Tomas/base_project_go/internal/errors"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
)

func TestTransactionManager_AllOrNothing(t *testing.T) {
	db := testDB(t)
	tx := NewMongoTransactionManager(db)
	holdings := NewMongoHoldingRepository(db)
	movements := NewMongoMovementRepository(db)
	ctx := context.Background()
	user := newUser()
	h := holding(user, "BTC", "Crypto", "Ledger", 100, at(1))

	// A failure after both writes leaves neither.
	boom := errors.New("boom")
	err := tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if _, err := holdings.Save(ctx, h); err != nil {
			return err
		}
		if err := movements.Save(ctx, movementOf(user, model.MovementOpening, &h, at(1))); err != nil {
			return err
		}
		return boom
	})
	assert.ErrorIs(t, err, boom, "fn's own error comes back as it is")
	got, err := holdings.FindAll(ctx, user)
	require.NoError(t, err)
	assert.Empty(t, got)
	page, err := movements.List(ctx, user, model.MovementFilter{}, nil, 10)
	require.NoError(t, err)
	assert.Empty(t, page.Items)

	// Success commits both.
	require.NoError(t, tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if _, err := holdings.Save(ctx, h); err != nil {
			return err
		}
		return movements.Save(ctx, movementOf(user, model.MovementOpening, &h, at(1)))
	}))
	got, _ = holdings.FindAll(ctx, user)
	assert.Len(t, got, 1)
	page, _ = movements.List(ctx, user, model.MovementFilter{}, nil, 10)
	assert.Len(t, page.Items, 1)

	assert.True(t, db.supportsTransactions(ctx), "the test database is a replica set")
	assert.False(t, db.supportsTransactions(cancelled()), "a failed check counts as no")
}

func TestTranslateTransactionError(t *testing.T) {
	standalone := mongodriver.CommandError{Code: 20, Name: "IllegalOperation", Message: "Transaction numbers are only allowed on a replica set member or mongos"}
	err := translateTransactionError(standalone)
	assert.ErrorAs(t, err, &appErrors.TransactionsUnavailableError{})
	assert.ErrorContains(t, err, "Run MongoDB as a replica set")

	for _, other := range []error{nil, errors.New("boom"), mongodriver.CommandError{Code: 20, Message: "something else"}, mongodriver.CommandError{Code: 112, Message: "Transaction numbers"}} {
		assert.Equal(t, other, translateTransactionError(other))
	}
}

func TestQuotaRepository_ReservesWithinTheLimit(t *testing.T) {
	db := testDB(t)
	quotas := NewMongoQuotaRepository(db)
	ctx := context.Background()
	user, other := newUser(), newUser()

	for i := 0; i < 3; i++ {
		ok, err := quotas.Reserve(ctx, user, "movements", 1, 3)
		require.NoError(t, err)
		assert.True(t, ok, i)
	}
	ok, err := quotas.Reserve(ctx, user, "movements", 1, 3)
	require.NoError(t, err)
	assert.False(t, ok, "the fourth goes over")
	ok, err = quotas.Reserve(ctx, other, "movements", 3, 3)
	require.NoError(t, err)
	assert.True(t, ok, "per user")

	require.NoError(t, quotas.Release(ctx, user, "movements", 1))
	ok, err = quotas.Reserve(ctx, user, "movements", 1, 3)
	require.NoError(t, err)
	assert.True(t, ok, "released room is reusable")

	_, err = quotas.Reserve(cancelled(), user, "movements", 1, 3)
	assert.Error(t, err)
	assert.Error(t, quotas.Release(cancelled(), user, "movements", 1))
	assert.Zero(t, count("not a number"))
	assert.Equal(t, int64(7), count(int64(7)))
}

func TestQuotaRepository_IsExactUnderConcurrentTransactions(t *testing.T) {
	db := testDB(t)
	tx := NewMongoTransactionManager(db)
	quotas := NewMongoQuotaRepository(db)
	user := newUser()
	const limit, racers = 5, 20

	var wg sync.WaitGroup
	results := make([]bool, racers)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := tx.WithinTransaction(context.Background(), func(ctx context.Context) error {
				ok, err := quotas.Reserve(ctx, user, "movements", 1, limit)
				results[i] = ok
				return err
			})
			assert.NoError(t, err)
		}(i)
	}
	wg.Wait()

	granted := 0
	for _, ok := range results {
		if ok {
			granted++
		}
	}
	assert.Equal(t, limit, granted, "exactly the limit, however the transactions interleave")
	var doc bson.M
	require.NoError(t, db.Quotas.FindOne(context.Background(), bson.M{"_id": user.String()}).Decode(&doc))
	assert.Equal(t, int64(limit), count(doc["movements"]))
}

func movementOf(user model.UserId, kind model.MovementKind, h *model.Holding, occurred time.Time) model.Movement {
	ref := model.RefOf(*h)
	return model.Movement{
		Id: model.NewMovementId(), UserId: user, Kind: kind, OccurredAt: occurred, Amount: model.MustMoneyFromFloat(10),
		Fee: model.ZeroMoney, Holding: &ref, CreatedAt: occurred,
	}
}

func TestMovementRepository_StoresAndReadsEveryField(t *testing.T) {
	db := testDB(t)
	repo := NewMongoMovementRepository(db)
	ctx := context.Background()
	user, other := newUser(), newUser()
	from, to := holding(user, "Savings", "Cash", "Santander", 100, at(1)), holding(user, "USD cash", "Cash", "Balanz", 0, at(1))
	prev, next := model.MustMoneyFromFloat(100), model.MustMoneyFromFloat(80.5)

	transfer := movementOf(user, model.MovementTransfer, &from, at(2))
	toRef := model.RefOf(to)
	transfer.ToHolding, transfer.Fee, transfer.Note = &toRef, model.MustMoneyFromFloat(2.5), "to the broker"
	adjustment := movementOf(user, model.MovementAdjustment, &from, at(3))
	adjustment.PreviousValue, adjustment.NewValue = &prev, &next
	for _, m := range []model.Movement{transfer, adjustment} {
		require.NoError(t, repo.Save(ctx, m))
	}

	got, err := repo.FindById(ctx, user, transfer.Id)
	require.NoError(t, err)
	assert.Equal(t, transfer.Kind, got.Kind)
	assert.Equal(t, "2.50", got.Fee.String())
	assert.Equal(t, "10.00", got.Amount.String())
	assert.Equal(t, *transfer.Holding, *got.Holding)
	assert.Equal(t, *transfer.ToHolding, *got.ToHolding)
	assert.Equal(t, "to the broker", got.Note)
	assert.True(t, transfer.OccurredAt.Equal(got.OccurredAt))
	assert.Nil(t, got.PreviousValue)

	got, err = repo.FindById(ctx, user, adjustment.Id)
	require.NoError(t, err)
	assert.Equal(t, "100.00", got.PreviousValue.String())
	assert.Equal(t, "80.50", got.NewValue.String())
	assert.True(t, got.Fee.IsZero())
	assert.Nil(t, got.ToHolding)

	got, err = repo.FindById(ctx, other, transfer.Id)
	require.NoError(t, err)
	assert.Nil(t, got, "someone else's movement is as missing as one that never was")
	deleted, err := repo.DeleteById(ctx, other, transfer.Id)
	require.NoError(t, err)
	assert.False(t, deleted)

	deleted, err = repo.DeleteById(ctx, user, transfer.Id)
	require.NoError(t, err)
	assert.True(t, deleted)
	got, err = repo.FindById(ctx, user, transfer.Id)
	require.NoError(t, err)
	assert.Nil(t, got)

	_, err = repo.FindById(cancelled(), user, adjustment.Id)
	assert.Error(t, err)
	_, err = repo.DeleteById(cancelled(), user, adjustment.Id)
	assert.Error(t, err)
	assert.Error(t, repo.Save(cancelled(), adjustment))
}

func TestMovementRepository_ListsNewestFirstAPageAtATime(t *testing.T) {
	db := testDB(t)
	repo := NewMongoMovementRepository(db)
	ctx := context.Background()
	user := newUser()
	a, b := holding(user, "A", "Cash", "Bank", 1, at(1)), holding(user, "B", "Cash", "Bank", 1, at(1))

	// Same occurredAt and createdAt for three of them: the id breaks the tie, so pages never skip or repeat.
	var all []model.Movement
	for i, spec := range []struct {
		kind     model.MovementKind
		h        *model.Holding
		occurred time.Time
	}{
		{model.MovementOpening, &a, at(1)}, {model.MovementGain, &a, at(5)}, {model.MovementLoss, &b, at(5)},
		{model.MovementDeposit, &a, at(5)}, {model.MovementWithdrawal, &b, at(9)},
	} {
		m := movementOf(user, spec.kind, spec.h, spec.occurred)
		m.CreatedAt = at(10)
		if i == 0 {
			m.CreatedAt = at(1)
		}
		require.NoError(t, repo.Save(ctx, m))
		all = append(all, m)
	}
	toB := movementOf(user, model.MovementTransfer, &a, at(7))
	bRef := model.RefOf(b)
	toB.ToHolding = &bRef
	require.NoError(t, repo.Save(ctx, toB))
	require.NoError(t, repo.Save(ctx, movementOf(newUser(), model.MovementGain, &a, at(8))), "another user's")

	var seen []model.MovementKind
	var after *model.MovementCursor
	for pages := 0; ; pages++ {
		page, err := repo.List(ctx, user, model.MovementFilter{}, after, 2)
		require.NoError(t, err)
		for _, m := range page.Items {
			seen = append(seen, m.Kind)
		}
		if page.Next == nil {
			assert.Equal(t, 2, pages, "three pages of 2")
			break
		}
		after = page.Next
	}
	require.Len(t, seen, 6)
	assert.Equal(t, model.MovementWithdrawal, seen[0], "newest first")
	assert.Equal(t, model.MovementTransfer, seen[1])
	assert.ElementsMatch(t, []model.MovementKind{model.MovementGain, model.MovementLoss, model.MovementDeposit}, seen[2:5])
	assert.Equal(t, model.MovementOpening, seen[5])

	list := func(filter model.MovementFilter) []model.MovementKind {
		t.Helper()
		page, err := repo.List(ctx, user, filter, nil, 50)
		require.NoError(t, err)
		var kinds []model.MovementKind
		for _, m := range page.Items {
			kinds = append(kinds, m.Kind)
		}
		return kinds
	}
	// B's log: its own movements and the transfer that arrived to it.
	assert.ElementsMatch(t, []model.MovementKind{model.MovementWithdrawal, model.MovementTransfer, model.MovementLoss}, list(model.MovementFilter{HoldingId: &b.Id}))
	assert.ElementsMatch(t, []model.MovementKind{model.MovementGain, model.MovementLoss}, list(model.MovementFilter{Kinds: []model.MovementKind{model.MovementGain, model.MovementLoss}}))
	from, until := at(5), at(7)
	assert.ElementsMatch(t, []model.MovementKind{model.MovementGain, model.MovementLoss, model.MovementDeposit, model.MovementTransfer}, list(model.MovementFilter{From: &from, To: &until}))

	_, err := repo.List(cancelled(), user, model.MovementFilter{}, nil, 2)
	assert.Error(t, err)
}

func TestMovementRepository_UnreadableDocuments(t *testing.T) {
	db := testDB(t)
	repo := NewMongoMovementRepository(db)
	user := newUser()
	base := func(over bson.M) bson.M {
		doc := bson.M{"_id": uuid.NewString(), "user_id": user.String(), "kind": "GAIN", "occurred_at": at(1), "amount_usd": "1.00", "created_at": at(1)}
		for k, v := range over {
			doc[k] = v
		}
		return doc
	}
	for name, doc := range map[string]bson.M{
		"bad id":         base(bson.M{"_id": "nope"}),
		"bad kind":       base(bson.M{"kind": "REFUND"}),
		"bad amount":     base(bson.M{"amount_usd": "lots"}),
		"bad fee":        base(bson.M{"fee_usd": "-1"}),
		"bad holding id": base(bson.M{"holding": bson.M{"id": "nope"}}),
		"bad to id":      base(bson.M{"to_holding": bson.M{"id": "nope"}}),
		"bad previous":   base(bson.M{"previous_value_usd": "x"}),
		"bad new":        base(bson.M{"new_value_usd": "x"}),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := db.Movements.DeleteMany(context.Background(), bson.M{"user_id": user.String()})
			require.NoError(t, err)
			insertRaw(t, db, "movements", doc)
			_, err = repo.List(context.Background(), user, model.MovementFilter{}, nil, 10)
			assert.Error(t, err)
		})
	}
	_, err := mapDocToMovement(movementDoc{ID: uuid.NewString(), UserID: "nope"})
	assert.Error(t, err)

	// Names a holding had are shown as stored, even if the domain wouldn't take them now.
	_, err = db.Movements.DeleteMany(context.Background(), bson.M{"user_id": user.String()})
	require.NoError(t, err)
	insertRaw(t, db, "movements", base(bson.M{"holding": bson.M{"id": uuid.NewString(), "name": "Old", "platform": "", "asset_class": ""}}))
	page, err := repo.List(context.Background(), user, model.MovementFilter{}, nil, 10)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, "Old", page.Items[0].Holding.Name)
}

func TestHoldingRepository_ExistingIds(t *testing.T) {
	db := testDB(t)
	repo := NewMongoHoldingRepository(db)
	ctx := context.Background()
	user, other := newUser(), newUser()
	mine, theirs := holding(user, "A", "Cash", "Bank", 1, at(1)), holding(other, "B", "Cash", "Bank", 1, at(1))
	for _, h := range []model.Holding{mine, theirs} {
		_, err := repo.Save(ctx, h)
		require.NoError(t, err)
	}
	gone := model.NewHoldingId()

	existing, err := repo.ExistingIds(ctx, user, []model.HoldingId{mine.Id, theirs.Id, gone})
	require.NoError(t, err)
	assert.Equal(t, map[model.HoldingId]bool{mine.Id: true}, existing)

	existing, err = repo.ExistingIds(ctx, user, nil)
	require.NoError(t, err)
	assert.Empty(t, existing)

	_, err = repo.ExistingIds(cancelled(), user, []model.HoldingId{mine.Id})
	assert.Error(t, err)
}

func TestEnsureIndexes_CoverTheActivityLog(t *testing.T) {
	db := testDB(t)
	cursor, err := db.Movements.Indexes().List(context.Background())
	require.NoError(t, err)
	var indexes []bson.M
	require.NoError(t, cursor.All(context.Background(), &indexes))
	var names []string
	for _, idx := range indexes {
		names = append(names, idx["name"].(string))
	}
	assert.ElementsMatch(t, []string{
		"_id_",
		"user_id_1_occurred_at_-1_created_at_-1__id_-1",
		"user_id_1_holding.id_1_occurred_at_-1_created_at_-1__id_-1",
		"user_id_1_to_holding.id_1_occurred_at_-1_created_at_-1__id_-1",
		"user_id_1_debt.id_1_occurred_at_-1_created_at_-1__id_-1",
	}, names)
}
