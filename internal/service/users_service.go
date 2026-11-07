package service

import (
	"context"
	"errors"

	"github.com/AzizAl-Soufi/go-todos-api/internal/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/middleware"
	"github.com/AzizAl-Soufi/go-todos-api/internal/repository/todos"
	"github.com/AzizAl-Soufi/go-todos-api/internal/repository/users"
	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/shared/errors"
)

type usersService struct {
	repo      users.UsersRepository
	todosRepo todos.TodosRepository
	jwt       *middleware.JWTMiddleware
}

func NewUsersService(
	repo users.UsersRepository,
	todosRepo todos.TodosRepository,
	jwt *middleware.JWTMiddleware,
) UsersService {
	return &usersService{repo: repo, todosRepo: todosRepo, jwt: jwt}
}

func (s *usersService) Register(ctx context.Context, dto *domain.UserDTO) (*domain.RegisterUserResponse, error) {
	user, err := s.repo.Register(ctx, dto)
	if err != nil {
		return nil, err
	}

	overview, err := s.buildOverview(ctx, user)
	if err != nil {
		return nil, err
	}

	tokens, err := s.issueTokens(user)
	if err != nil {
		return nil, err
	}

	return &domain.RegisterUserResponse{Authorization: tokens, Overview: overview}, nil
}

func (s *usersService) RefreshToken(ctx context.Context, params *domain.RefreshRequest) (*middleware.TokenPair, error) {
	claims, err := s.jwt.ValidateRefreshToken(params.RefreshToken)
	if err != nil {
		return nil, err
	}
	if claims.CustomerInfo == nil {
		return nil, middleware.ErrExpiredRefreshToken
	}

	user, err := s.repo.Auth(ctx, claims.CustomerInfo.ID)
	if err != nil {
		return nil, err
	}

	return s.issueTokens(user)
}

func (s *usersService) Auth(ctx context.Context) (*domain.Overview, error) {
	return s.GetOverview(ctx)
}

func (s *usersService) GetOverview(ctx context.Context) (*domain.Overview, error) {
	claims, err := middleware.AuthorizationFrom(ctx)
	if err != nil {
		return nil, err
	}

	user, err := s.repo.GetOverview(ctx, claims.ID)
	if err != nil {
		return nil, err
	}

	return s.buildOverview(ctx, user)
}

func (s *usersService) DeleteAccount(ctx context.Context) error {
	claims, err := middleware.AuthorizationFrom(ctx)
	if err != nil {
		return err
	}

	if err := s.todosRepo.DeleteByUserID(ctx, claims.ID); err != nil && !errors.Is(err, apperrors.ErrNotFound) {
		return err
	}

	return s.repo.Delete(ctx, claims.ID)
}

// buildOverview assembles the user's overview. A just-created user has no
// todos, so the lookup is skipped; "not found" from the repo means none.
func (s *usersService) buildOverview(ctx context.Context, user *domain.User) (*domain.Overview, error) {
	todos := []*domain.Todo{}
	if !user.IsNew {
		found, err := s.todosRepo.GetAll(ctx, user.ID)
		if err != nil && !errors.Is(err, apperrors.ErrNotFound) {
			return nil, err
		}
		if found != nil {
			todos = found
		}
	}

	return &domain.Overview{
		ID:    user.ID,
		Name:  user.Name,
		Email: user.Email,
		Todos: todos,
	}, nil
}

func (s *usersService) issueTokens(user *domain.User) (*middleware.TokenPair, error) {
	tokens, err := s.jwt.GenerateTokenPair(middleware.NewAuthorization(user.ID, user.Name, user.Email))
	if err != nil {
		// A signing failure is a server problem, not the client's: report 500.
		return nil, apperrors.InternalWrap("TOKEN_GENERATION_FAILED", "failed to generate tokens", err)
	}
	return tokens, nil
}
