package policy

type PolicyError struct {
	Message string
}

func (e PolicyError) Error() string {
	return e.Message
}

func Reject(reason string) map[string]any {
	return map[string]any{
		"decision": "REJECTED",
		"status":   "POLICY_REJECTED",
		"message":  reason,
	}
}

func InternalError(err error) map[string]any {
	return map[string]any{
		"decision": "ERROR",
		"status":   "ERROR",
		"message":  err.Error(),
	}
}

func NewError(message string) PolicyError {
	return PolicyError{Message: message}
}
