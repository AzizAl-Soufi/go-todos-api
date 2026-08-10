package mongodb

import (
	"context"
	"fmt"
	"time"

	"github.com/AzizAl-Soufi/todos-api/internal/common/config"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type MongoDBClient interface {
	Ping(ctx context.Context) error
	Database() *mongo.Database
	Close(ctx context.Context) error
}

type MongoDB struct {
	client   *mongo.Client
	database *mongo.Database
}

var _ MongoDBClient = (*MongoDB)(nil)

func New(ctx context.Context, config *config.DatabaseConfig) (MongoDBClient, error) {
	db := &MongoDB{}
	if err := db.connect(ctx, config.MongoURI, config.MongoDBN); err != nil {
		return nil, err
	}
	return db, nil
}

func (m *MongoDB) connect(_ context.Context, uri, dbname string) error {
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return fmt.Errorf("failed to connect to mongodb: %w", err)
	}
	m.client = client
	m.database = client.Database(dbname)
	return nil
}

func (m *MongoDB) Database() *mongo.Database {
	return m.database
}

func (m *MongoDB) Ping(ctx context.Context) error {
	if m.client == nil {
		return fmt.Errorf("client not connected")
	}
	return m.client.Ping(ctx, nil)
}

func (m *MongoDB) Close(ctx context.Context) error {
	if m.client == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return m.client.Disconnect(ctx)
}
