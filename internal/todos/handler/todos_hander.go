package handler

import (
	"encoding/json"
	"net/http"

	"github.com/AzizAl-Soufi/todos-api/internal/todos/domain"
)

type TodosHandler struct {
	svc domain.TodosService
}

func NewTodosHandler(svc domain.TodosService) *TodosHandler {
	return &TodosHandler{svc: svc}
}

func (h *TodosHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	todo, err := h.svc.CreateTodo(r.Context(), req.Title)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(todo)
}

func (h *TodosHandler) GettAll(w http.ResponseWriter, r *http.Request) {
	todos, err := h.svc.GetTodos(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(todos)
}
