package service

import (
	"context"
	"errors"

	"github.com/AzizAl-Soufi/go-todos-api/internal/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/repository/todos"
	"github.com/AzizAl-Soufi/go-todos-api/internal/repository/users"
	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/shared/errors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/middleware"
)

type usersService struct {
	repo      users.UsersRepository
	todosRepo todos.TodosRepository
	jwt       *middleware.JWTMiddleware
}

func authorizationForUser(user *domain.User) *middleware.Authorization {
	return middleware.NewAuthorization(user.ID, user.Name, user.Email)
}

func NewUsersService(
	repo users.UsersRepository,
	todosRepo todos.TodosRepository,
	jwt *middleware.JWTMiddleware,
) UsersService {
	return &usersService{repo: repo, todosRepo: todosRepo, jwt: jwt}
}

func (s *usersService) Register(ctx context.Context, user *domain.UserDTO) (*domain.RegisterUserResponse, error) {
	userObject, err := s.repo.Register(ctx, user)
	if err != nil {
		return nil, err
	}

	var todos []*domain.Todo
	if userObject.IsNew == true {
		todos = make([]*domain.Todo, 0)
	} else {
		todos, err = s.todosRepo.GetAll(ctx, userObject.ID)
		if err != nil {
			if !errors.Is(err, apperrors.ErrNotFound) {
				return nil, err
			}
			todos = []*domain.Todo{}
		}
	}

	tokens, err := s.jwt.GenerateTokenPair(authorizationForUser(userObject))
	if err != nil {
		return nil, apperrors.Unauthorizedf("UNKNOWN_ERROR", "Failed to generate tokens: %v", err.Error())
	}

	overview := &domain.Overview{
		ID:    userObject.ID,
		Name:  userObject.Name,
		Email: user.Email,
		Todos: todos,
	}

	return &domain.RegisterUserResponse{Authorization: tokens, Overview: overview}, nil
}

func (s *usersService) RefreshToken(ctx context.Context, params *domain.RefreshRequest) (*middleware.TokenPair, error) {

	claims, err := s.jwt.ValidateRefreshToken(params.RefreshToken)
	if err != nil {
		return nil, err
	}

	userObject, err := s.repo.Auth(ctx, claims.CustomerInfo.ID)
	if err != nil {
		return nil, err
	}

	tokens, err := s.jwt.GenerateTokenPair(
		authorizationForUser(userObject),
	)
	if err != nil {
		return nil, apperrors.Unauthorizedf("UNKNOWN_ERROR", "Failed to generate tokens: %v", err.Error())
	}

	return tokens, nil
}

func (s *usersService) Auth(ctx context.Context) (*domain.Overview, error) {
	claims, ok := middleware.GetAuthorization(ctx)
	if !ok {
		return nil, middleware.ErrUnauthorizedContext
	}

	userObject, err := s.repo.GetOverview(ctx, claims.ID)
	if err != nil {
		return nil, err
	}

	var todos []*domain.Todo
	if userObject.IsNew == true {
		todos = make([]*domain.Todo, 0)
	} else {
		todos, err = s.todosRepo.GetAll(ctx, userObject.ID)
		if err != nil {
			if !errors.Is(err, apperrors.ErrNotFound) {
				return nil, err

			}
			todos = []*domain.Todo{}
		}
	}

	overview := &domain.Overview{
		ID:    userObject.ID,
		Name:  userObject.Name,
		Email: userObject.Email,
		Todos: todos,
	}

	return overview, nil
}

func (s *usersService) GetOverview(ctx context.Context) (*domain.Overview, error) {
	claims, ok := middleware.GetAuthorization(ctx)
	if !ok {
		return nil, middleware.ErrUnauthorizedContext
	}

	user, err := s.repo.GetOverview(ctx, claims.ID)
	if err != nil {
		return nil, err
	}

	todos, err := s.todosRepo.GetAll(ctx, user.ID)
	if err != nil {
		if !errors.Is(err, apperrors.ErrNotFound) {
			return nil, err

		}
		todos = []*domain.Todo{}
	}

	overview := domain.Overview{
		ID:    user.ID,
		Name:  user.Name,
		Email: user.Email,
		Todos: todos,
	}
	return &overview, nil
}

func (s *usersService) DeleteAccount(ctx context.Context) error {
	claims, ok := middleware.GetAuthorization(ctx)
	if !ok {
		return middleware.ErrUnauthorizedContext
	}

	if err := s.todosRepo.DeleteByUserID(ctx, claims.ID); err != nil {
		if !errors.Is(err, apperrors.ErrNotFound) {
			return err
		}
	}

	if err := s.repo.Delete(ctx, claims.ID); err != nil {
		return err
	}

	return nil
}
