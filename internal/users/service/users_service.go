package service

import (
	"context"
	"errors"
	"fmt"

	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/common/errors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/common/middleware"
	"github.com/AzizAl-Soufi/go-todos-api/internal/users/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/users/repository"

	todos_domain "github.com/AzizAl-Soufi/go-todos-api/internal/todos/domain"
	todos_repo "github.com/AzizAl-Soufi/go-todos-api/internal/todos/repository"
)

type usersService struct {
	repo      repository.UsersRepository
	todosRepo todos_repo.TodosRepository
	jwt       *middleware.JWTMiddleware
}

func NewUsersService(
	repo repository.UsersRepository,
	todosRepo todos_repo.TodosRepository,
	jwt *middleware.JWTMiddleware,
) UsersService {
	return &usersService{repo: repo, todosRepo: todosRepo, jwt: jwt}
}

func (s *usersService) Register(ctx context.Context, user *domain.UserDTO) (*domain.RegisterUserResponse, error) {
	userObject, err := s.repo.Register(ctx, user)
	if err != nil {
		return nil, err
	}

	var todos []*todos_domain.Todo
	if userObject.IsNew == true {
		todos = make([]*todos_domain.Todo, 0)
	} else {
		todos, err = s.todosRepo.GetAll(ctx, userObject.ID)
		if err != nil {
			if !errors.Is(err, apperrors.ErrNotFound) {
				return nil, err
			}
			todos = []*todos_domain.Todo{}
		}
	}

	tokens, err := s.jwt.GenerateTokenPair(
		middleware.NewAuthorization(userObject.ID, userObject.Name, userObject.Email),
	)
	if err != nil {
		return nil, fmt.Errorf("Failed to generate tokens: %v", err.Error())
	}

	overview := &domain.Overview{
		ID:    userObject.ID,
		Name:  userObject.Name,
		Email: user.Email,
		Todos: todos,
	}

	return &domain.RegisterUserResponse{
		Authorization: tokens,
		User:          &domain.RegisterUserData{User: userObject, Overview: overview},
	}, nil
}

func (s *usersService) RefreshToken(ctx context.Context, params *domain.RefreshRequest) (*middleware.TokenPair, error) {

	claims, err := s.jwt.ValidateRefreshToken(params.RefreshToken)
	if err != nil {
		return nil, err
	}

	userObject, err := s.repo.Auth(ctx, claims.CustomerInfo.Email)
	if err != nil {
		return nil, err
	}

	tokens, err := s.jwt.GenerateTokenPair(
		middleware.NewAuthorization(userObject.ID, userObject.Name, userObject.Email),
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

	userObject, err := s.repo.GetOverview(ctx, claims.Email)
	if err != nil {
		return nil, err
	}

	var todos []*todos_domain.Todo
	if userObject.IsNew == true {
		todos = make([]*todos_domain.Todo, 0)
	} else {
		todos, err = s.todosRepo.GetAll(ctx, userObject.ID)
		if err != nil {
			if !errors.Is(err, apperrors.ErrNotFound) {
				return nil, err

			}
			todos = []*todos_domain.Todo{}
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

	user, err := s.repo.GetOverview(ctx, claims.Email)
	if err != nil {
		return nil, err
	}

	todos, err := s.todosRepo.GetAll(ctx, user.ID)
	if err != nil {
		if !errors.Is(err, apperrors.ErrNotFound) {
			return nil, err

		}
		todos = []*todos_domain.Todo{}
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

	if err := s.repo.Delete(ctx, claims.Email); err != nil {
		return err
	}

	return nil
}
