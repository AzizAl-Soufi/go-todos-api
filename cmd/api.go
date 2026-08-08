package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"time"

	"github.com/AzizAl-Soufi/todos-api/internal/common/config"
	"github.com/AzizAl-Soufi/todos-api/internal/pkg/database/mongodb"
	"github.com/AzizAl-Soufi/todos-api/internal/todos/handler"
	"github.com/AzizAl-Soufi/todos-api/internal/todos/repository"
	services "github.com/AzizAl-Soufi/todos-api/internal/todos/service"
)

type application struct {
	config *config.Config
	repo   repository.TodosRepository
}

func NewApplication(ctx context.Context, cfg *config.Config) (application, func()) {
	var todosRepo repository.TodosRepository
	var cleanup func() = func() {} // Default to a no-op function

	dbType := cfg.DBType
	if dbType == "" {
		dbType = "memory"
	}

	switch dbType {
	case "mongo":
		uri := cfg.MongoURI
		dbName := cfg.MongoDBN
		if uri == "" || dbName == "" {
			log.Fatal("Set your mongodb connection string in environment variables.")
		}

		db, err := mongodb.New(ctx, cfg)
		if err != nil {
			log.Fatalf("Failed to connect to MongoDB: %v", err)
		}

		if err := db.Ping(ctx); err != nil {
			log.Fatalf("Failed to ping MongoDB: %v", err)
		}

		cleanup = func() {
			slog.Info("Closing MongoDB connection...")
			disconnectCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := db.Close(disconnectCtx); err != nil {
				slog.Error("Error disconnecting from MongoDB", "error", err)
			}
		}

		todosRepo = repository.NewMongoTodosRepository(db)

	case "memory":
		todosRepo = repository.NewInMemoryTodosRepository()

	default:
		log.Fatalf("unsupported DB_TYPE: %s", dbType)
	}

	app := application{
		config: cfg,
		repo:   todosRepo,
	}

	return app, cleanup
}

func (app *application) initialize() http.Handler {
	r := http.NewServeMux()
	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	todosService := services.NewTodosService(app.repo)
	todosHandler := handler.NewTodosHandler(todosService)

	r.HandleFunc("POST /todos", todosHandler.Create)
	r.HandleFunc("GET /todos", todosHandler.GettAll)
	r.HandleFunc("GET /todos/{id}", todosHandler.Get)
	r.HandleFunc("PUT /todos/{id}", todosHandler.Update)
	r.HandleFunc("DELETE /todos/{id}", todosHandler.Delete)
	return r
}

func (app *application) run(ctx context.Context, h http.Handler) error {
	port := fmt.Sprintf(":%v", app.config.Port)
	server := &http.Server{
		Addr:         port,
		Handler:      h,
		WriteTimeout: time.Second * 30,
		ReadTimeout:  time.Second * 10,
		IdleTimeout:  time.Minute,
	}

	slog.Info("Server has started", "addr", port)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("Server shutdown failed", "error", err)
		}
	}()

	err := server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}

	return err
}
