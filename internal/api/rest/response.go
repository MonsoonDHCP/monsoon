package rest

import (
	"encoding/json"
	"net/http"
)

type errorCodeSetter interface {
	SetErrorCode(code string)
}

type Envelope struct {
	Data  any       `json:"data,omitempty"`
	Meta  any       `json:"meta,omitempty"`
	Error *APIError `json:"error,omitempty"`
}

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func WriteJSON(w http.ResponseWriter, status int, data any, meta any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Envelope{Data: data, Meta: meta})
}

func WriteError(w http.ResponseWriter, status int, code string, msg string) {
	if setter, ok := w.(errorCodeSetter); ok {
		setter.SetErrorCode(code)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Envelope{Error: &APIError{Code: code, Message: msg}})
}
