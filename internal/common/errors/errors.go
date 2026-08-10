package apperrors

var (
	ErrNotFound = NotFound(
		"NOT_FOUND",
		"entity not found",
	)

	ErrDuplicate = Conflict(
		"DUPLICATE_ENTITY",
		"entity already exists",
	)
)
