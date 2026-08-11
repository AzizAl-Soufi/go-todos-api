package apperrors

var (
	ErrInvalidRequestBody = Validation(
		"INVALID_BODY",
		"invalid request body",
	)

	ErrInvalidRequestValue = Validation(
		"INVALID_VALUE",
		"invalid request value",
	)

	ErrInvalidEntity = Validation(
		"INVALID_ENTITY",
		"invalid entity",
	)
)
