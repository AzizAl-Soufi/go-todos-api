package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/common"
	"github.com/AzizAl-Soufi/go-todos-api/internal/common/config"
	"github.com/AzizAl-Soufi/go-todos-api/internal/common/middleware"
	"github.com/AzizAl-Soufi/go-todos-api/internal/pkg/database/mongodb"

	todosHandler "github.com/AzizAl-Soufi/go-todos-api/internal/todos/handler"
	todosRepository "github.com/AzizAl-Soufi/go-todos-api/internal/todos/repository"
	todosService "github.com/AzizAl-Soufi/go-todos-api/internal/todos/service"

	usersHandler "github.com/AzizAl-Soufi/go-todos-api/internal/users/handler"
	usersRepository "github.com/AzizAl-Soufi/go-todos-api/internal/users/repository"
	usersService "github.com/AzizAl-Soufi/go-todos-api/internal/users/service"
)

type application struct {
	config    *config.Config
	jwt       *middleware.JWTMiddleware
	todosRepo todosRepository.TodosRepository
	usersRepo usersRepository.UsersRepository
}

func NewApplication(ctx context.Context, cfg *config.Config) (application, func()) {
	var todosRepo todosRepository.TodosRepository
	var usersRepo usersRepository.UsersRepository
	var cleanup func() = func() {} // Default to a no-op function

	dbCfg := cfg.DB
	dbType := dbCfg.DBType
	if dbType == "" {
		dbType = "memory"
	}

	switch dbType {
	case "mongo":
		uri := dbCfg.MongoURI
		dbName := dbCfg.MongoDBN
		if uri == "" || dbName == "" {
			log.Fatal("Set your mongodb connection string in environment variables.")
		}

		db, err := mongodb.New(ctx, &dbCfg)
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

		todosRepo = todosRepository.NewMongoTodosRepository(db)
		usersRepo = usersRepository.NewMongoUsersRepository(db)

	case "memory":
		todosRepo = todosRepository.NewInMemoryTodosRepository()
		usersRepo = usersRepository.NewInMemoryUsersRepository()

	default:
		log.Fatalf("unsupported DB_TYPE: %s", dbType)
	}

	jwt, err := middleware.NewJWTMiddleware(&cfg.JWT)
	if err != nil {
		log.Fatalf("Failed to Initialize JWT: %v", err)
	}

	app := application{
		config:    cfg,
		todosRepo: todosRepo,
		usersRepo: usersRepo,
		jwt:       jwt,
	}

	return app, cleanup
}

func (app *application) initialize() http.Handler {
	r := http.NewServeMux()
	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		
		w.Write([]byte("ok"))
	})

	todosService := todosService.NewTodosService(app.todosRepo, app.usersRepo)
	todosHandler := todosHandler.NewTodosHandler(todosService)

	usersService := usersService.NewUsersService(app.usersRepo, app.todosRepo, app.jwt)
	usersHandler := usersHandler.NewUsersHandler(usersService)

	r.HandleFunc("POST /register", usersHandler.Register)
	r.HandleFunc("POST /refresh-token", usersHandler.RefreshToken)

	protectedRouter := http.NewServeMux()

	protectedRouter.HandleFunc("POST /auth", usersHandler.Auth)
	protectedRouter.HandleFunc("DELETE /user", usersHandler.Delete)
	protectedRouter.HandleFunc("GET /overview", usersHandler.GetOverview)
	protectedRouter.HandleFunc("POST /todos", todosHandler.Create)
	protectedRouter.HandleFunc("GET /todos", todosHandler.GettAll)
	protectedRouter.HandleFunc("GET /todos/{id}", todosHandler.Get)
	protectedRouter.HandleFunc("PUT /todos/{id}", todosHandler.Update)
	protectedRouter.HandleFunc("DELETE /todos/{id}", todosHandler.Delete)

	r.Handle("/", app.jwt.RequireAuth(protectedRouter))
	return common.Recovery(r)
}

func (app *application) run(ctx context.Context, h http.Handler) error {
	port := fmt.Sprintf(":%v", app.config.App.Port)
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
