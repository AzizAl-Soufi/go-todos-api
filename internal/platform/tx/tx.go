// Package tx defines the unit-of-work port shared by all modules.
//
// A Runner executes fn inside one database transaction. The transaction travels
// in the context, so every repository call made with the ctx passed to fn
// joins it, even across modules. Adapters live next to each database client.
package tx

import "context"

type Runner interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// None runs fn without a transaction. Used by the in-memory backend and as a
// fallback where the database cannot provide transactions.
type None struct{}

func (None) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}
