package apperrors

type AppError struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	Recoverable bool   `json:"recoverable"`
	cause       error
}

func New(code string, message string, recoverable bool) *AppError {
	return &AppError{
		Code:        code,
		Message:     message,
		Recoverable: recoverable,
	}
}

// NewWithDetails is retained for source compatibility. Details are deliberately
// not exposed by the public error contract.
func NewWithDetails(code string, message string, recoverable bool, _ map[string]any) *AppError {
	return New(code, message, recoverable)
}

func Wrap(code string, message string, recoverable bool, cause error) *AppError {
	return &AppError{
		Code:        code,
		Message:     message,
		Recoverable: recoverable,
		cause:       cause,
	}
}

func (e *AppError) Error() string {
	if e == nil {
		return ""
	}
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

func (e *AppError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}
