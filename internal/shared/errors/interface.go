package apperrors

type AppError interface {
	Status() int
	Code() string
	Message() string
	Unwrap() error
	error
}
