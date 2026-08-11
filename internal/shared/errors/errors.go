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
	
	ErrExistingUser = Conflict(
		"USER_EXISTS", 
		"user already exists",
	)
)
