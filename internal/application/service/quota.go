package service

import "context"

// confirmUnderCap runs right after an insert. The cap check before it isn't atomic with the insert, so
// concurrent creates can all pass it: re-count with ours in and withdraw ours on an overrun. Of any racing
// inserts, the last one that stays has counted every other one that stays, so the user never ends up above
// the cap. Fail-closed by design: when a burst crosses the cap every racer may be withdrawn, even one that
// would have fit (a retry then succeeds), and ours is withdrawn too if the re-count fails. It runs detached
// from the request, so a client hanging up right after its insert can't skip it. If even the withdrawal
// fails, the insert stands and is reported as created: a failure would only invite a duplicate retry.
//
// It returns nil when the insert stands, else the error to report.
func confirmUnderCap(
	ctx context.Context,
	limit int,
	limitErr error,
	count func(context.Context) (int64, error),
	withdraw func(context.Context) error,
) error {
	detached := context.WithoutCancel(ctx)
	n, err := count(detached)
	if err == nil && n <= int64(limit) {
		return nil
	}
	if withdraw(detached) != nil {
		return nil
	}
	if err != nil {
		return err
	}
	return limitErr
}
