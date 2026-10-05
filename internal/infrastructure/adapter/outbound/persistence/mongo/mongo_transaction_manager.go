package mongo

import (
	"context"
	"errors"
	"strings"

	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	appErrors "github.com/GM-Tomas/base_project_go/internal/errors"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// illegalOperationCode is what a standalone mongod answers the first operation of a transaction with.
const illegalOperationCode = 20

type MongoTransactionManager struct {
	client *mongo.Client
}

func NewMongoTransactionManager(db *MongoDB) *MongoTransactionManager {
	return &MongoTransactionManager{client: db.Client}
}

var _ outbound.TransactionManager = (*MongoTransactionManager)(nil)

// WithinTransaction runs fn in a transaction of its own session; the driver retries it on transient errors
// (a write conflict with a concurrent transaction) and on an unknown commit result. fn's own error is
// returned as it is, after the transaction is aborted.
func (m *MongoTransactionManager) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	session, err := m.client.StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(context.WithoutCancel(ctx))

	_, err = session.WithTransaction(ctx, func(ctx context.Context) (any, error) {
		return nil, fn(ctx)
	})
	return translateTransactionError(err)
}

// translateTransactionError says plainly that the database can't run transactions, instead of a 500.
func translateTransactionError(err error) error {
	var cmdErr mongo.CommandError
	if errors.As(err, &cmdErr) && cmdErr.Code == illegalOperationCode && strings.Contains(cmdErr.Message, "Transaction numbers") {
		return appErrors.NewTransactionsUnavailableError(
			"This database doesn't support transactions. Run MongoDB as a replica set (see the README).")
	}
	return err
}
