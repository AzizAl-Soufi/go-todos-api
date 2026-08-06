package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/AzizAl-Soufi/todos-api/internal/core/config"
	"github.com/AzizAl-Soufi/todos-api/internal/pkg/mongodb"
	"github.com/AzizAl-Soufi/todos-api/internal/todos/handler"
	"github.com/AzizAl-Soufi/todos-api/internal/todos/repository"
	services "github.com/AzizAl-Soufi/todos-api/internal/todos/service"
)

type application struct {
	config *config.Config
	db     mongodb.MongoDBClient
}

func (app *application) initialize() http.Handler {
	r := http.NewServeMux()
	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	todosRepo := repository.NewMongoTodosRepository(app.db)
	todosService := services.NewTodosService(todosRepo)
	todosHandler := handler.NewTodosHandler(todosService)

	r.HandleFunc("POST /todos", todosHandler.Create)
	r.HandleFunc("GET /todos", todosHandler.GettAll)
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
