package common

import "errors"

var (
	ErrInvalidRequestTitleValue = errors.New("invalid title value")
	ErrInvalidRequestBody       = errors.New("invalid request body")
	ErrInvalidRequestValue      = errors.New("invalid request value")
	ErrNotFound                 = errors.New("entity not found")
	ErrDuplicate                = errors.New("entity already exists")
	ErrInvalidEntity            = errors.New("invalid entity")
)
