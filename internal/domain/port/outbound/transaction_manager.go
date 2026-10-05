package outbound

import "context"

// TransactionManager runs fn as one unit: what the repositories do with the ctx fn gets takes effect
// entirely or not at all. fn may run more than once (a write conflict with a concurrent request is retried),
// so it must not do anything outside the database, and must use that ctx for every call in it, one at a
// time (a transaction isn't safe for concurrent use).
type TransactionManager interface {
	WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}
