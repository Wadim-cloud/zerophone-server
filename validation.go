package main

import (
	"encoding/json"
	"net/http"
)

// ValidateRegister validates a registration request
func ValidateRegister(req RegisterRequest) error {
	if req.ID == "" {
		return &ValidationError{field: "id", message: "node ID is required"}
	}
	if len(req.ID) > 64 {
		return &ValidationError{field: "id", message: "node ID must be <= 64 characters"}
	}
	if req.Name == "" {
		return &ValidationError{field: "name", message: "name is required"}
	}
	if len(req.Name) > 128 {
		return &ValidationError{field: "name", message: "name must be <= 128 characters"}
	}
	// Validate capabilities
	for _, cap := range req.Capabilities {
		if len(cap) > 32 {
			return &ValidationError{field: "capabilities", message: "capability values must be <= 32 characters"}
		}
	}
	return nil
}

// ValidateSignal validates a signal request
func ValidateSignal(req SignalRequest) error {
	if req.FromID == "" {
		return &ValidationError{field: "from_id", message: "from_id is required"}
	}
	if len(req.FromID) > 64 {
		return &ValidationError{field: "from_id", message: "from_id must be <= 64 characters"}
	}
	if req.ToID == "" {
		return &ValidationError{field: "to_id", message: "to_id is required"}
	}
	if len(req.ToID) > 64 {
		return &ValidationError{field: "to_id", message: "to_id must be <= 64 characters"}
	}
	if req.Type == "" {
		return &ValidationError{field: "type", message: "type is required"}
	}
	validTypes := map[string]bool{
		MsgCallRequest: true,
		MsgCallAccept:  true,
		MsgCallReject:  true,
		MsgCallEnd:     true,
		MsgMessage:     true,
	}
	if !validTypes[req.Type] {
		return &ValidationError{field: "type", message: "invalid message type"}
	}
	// Call-related validations
	if req.Type == MsgCallRequest || req.Type == MsgCallAccept || req.Type == MsgCallEnd {
		if req.CallID == "" {
			return &ValidationError{field: "call_id", message: "call_id is required for call messages"}
		}
		if len(req.CallID) > 128 {
			return &ValidationError{field: "call_id", message: "call_id must be <= 128 characters"}
		}
	}
	return nil
}

type ValidationError struct {
	field   string
	message string
}

func (e *ValidationError) Error() string {
	return e.field + ": " + e.message
}

// RespondWithError sends a validation error response
func RespondWithError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"error":   err.Error(),
		"status":  "error",
	})
}
