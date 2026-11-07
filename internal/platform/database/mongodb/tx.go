package mongodb

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/tx"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// TxRunner implements tx.Runner with MongoDB multi-document transactions.
// Operations executed with the ctx passed to fn join the transaction.
type TxRunner struct {
	client *mongo.Client
}

// NewTxRunner returns a transactional runner when the deployment supports
// transactions (replica set or sharded cluster). A standalone server cannot, so
// it falls back to tx.None and logs a warning: work then runs without atomicity.
func NewTxRunner(ctx context.Context, db MongoDBClient) tx.Runner {
	ok, err := supportsTransactions(ctx, db.Database())
	if err != nil {
		slog.Warn("could not detect MongoDB topology; running without transactions", "error", err)
		return tx.None{}
	}
	if !ok {
		slog.Warn("MongoDB is a standalone server; multi-document transactions are disabled (use a replica set for atomic account deletion)")
		return tx.None{}
	}
	return TxRunner{client: db.Client()}
}

func (r TxRunner) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if mongo.SessionFromContext(ctx) != nil {
		return fn(ctx) // already inside a transaction
	}

	sess, err := r.client.StartSession()
	if err != nil {
		return fmt.Errorf("start session: %w", err)
	}
	defer sess.EndSession(ctx)

	_, err = sess.WithTransaction(ctx, func(txCtx context.Context) (any, error) {
		return nil, fn(txCtx)
	})
	return err
}

func supportsTransactions(ctx context.Context, db *mongo.Database) (bool, error) {
	var hello struct {
		SetName string `bson:"setName"`
		Msg     string `bson:"msg"`
	}
	if err := db.RunCommand(ctx, bson.D{{Key: "hello", Value: 1}}).Decode(&hello); err != nil {
		return false, err
	}
	return hello.SetName != "" || hello.Msg == "isdbgrid", nil
}
