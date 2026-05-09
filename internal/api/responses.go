package api

import (
	"encoding/json"
	"net/http"
)

type envelope struct {
	Data  any        `json:"data"`
	Error *apiError  `json:"error"`
	Meta  metaFields `json:"meta"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type metaFields struct {
	RequestID string `json:"request_id"`
}

func writeJSON(w http.ResponseWriter, status int, data any, requestID string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(envelope{
		Data:  data,
		Error: nil,
		Meta:  metaFields{RequestID: requestID},
	})
}

func writeError(w http.ResponseWriter, status int, code, message, requestID string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(envelope{
		Data:  nil,
		Error: &apiError{Code: code, Message: message},
		Meta:  metaFields{RequestID: requestID},
	})
}
