package api

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
)

var requestID atomic.Uint64

type envelopeOK struct {
	OK       bool   `json:"ok"`
	Data     any    `json:"data"`
	Revision string `json:"revision,omitempty"`
}

type envelopeError struct {
	OK        bool         `json:"ok"`
	Error     errorPayload `json:"error"`
	RequestID uint64       `json:"request_id"`
	Revision  string       `json:"revision,omitempty"`
}

type errorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeOK(w http.ResponseWriter, status int, data any, revision string) {
	writeJSON(w, status, envelopeOK{
		OK:       true,
		Data:     data,
		Revision: revision,
	})
}

func writeError(w http.ResponseWriter, status int, code, message, revision string) {
	writeJSON(w, status, envelopeError{
		OK: false,
		Error: errorPayload{
			Code:    code,
			Message: message,
		},
		RequestID: requestID.Add(1),
		Revision:  revision,
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(payload)
}
