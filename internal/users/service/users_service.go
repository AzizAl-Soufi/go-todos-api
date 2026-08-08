package service

import (
	"context"
	"errors"
	"time"

	"github.com/AzizAl-Soufi/todos-api/internal/common"
	"github.com/AzizAl-Soufi/todos-api/internal/users/domain"
	"github.com/AzizAl-Soufi/todos-api/internal/users/repository"

	todos_domain "github.com/AzizAl-Soufi/todos-api/internal/todos/domain"
	todos_repo "github.com/AzizAl-Soufi/todos-api/internal/todos/repository"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type usersService struct {
	repo      repository.UsersRepository
	todosRepo todos_repo.TodosRepository
}

func NewUsersService(repo repository.UsersRepository, todosRepo todos_repo.TodosRepository) UsersService {
	return &usersService{repo: repo, todosRepo: todosRepo}
}

func (s *usersService) Auth(ctx context.Context, user *domain.UserDTO) (*domain.Overview, error) {

	userObject, err := s.repo.Auth(ctx, &domain.User{
		ID:        bson.NewObjectID(),
		Name:      user.Name,
		Email:     user.Email,
		CreatedAt: time.Now(),
	})
	if err != nil {
		return nil, err
	}

	var todos []*todos_domain.Todo
	if userObject.IsNew == true {
		todos = make([]*todos_domain.Todo, 0)
	} else {
		todos, err = s.todosRepo.GetByUserID(ctx, userObject.ID)
		if err != nil {
			if !errors.Is(err, common.ErrNotFound) {
				return nil, err

			}
			todos = []*todos_domain.Todo{}
		}
	}

	overview := domain.Overview{
		ID:    userObject.ID,
		Name:  userObject.Name,
		Email: user.Email,
		Todos: todos,
	}

	return &overview, nil
}

func (s *usersService) GetOverview(ctx context.Context, id bson.ObjectID) (*domain.Overview, error) {
	user, err := s.repo.GetOverview(ctx, id)
	if err != nil {
		return nil, err
	}

	todos, err := s.todosRepo.GetByUserID(ctx, user.ID)
	if err != nil {
		if !errors.Is(err, common.ErrNotFound) {
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

func (s *usersService) DeleteAccount(ctx context.Context, id bson.ObjectID) error {
	if err := s.todosRepo.DeleteByUserID(ctx, id); err != nil {
		if !errors.Is(err, common.ErrNotFound) {
			return err
		}
	}

	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}

	return nil
}
