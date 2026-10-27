package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// MaxJSONBody is the default limit for JSON request bodies.
const MaxJSONBody = 1 << 20

// WriteJSON writes v as JSON with status. Errors writing are ignored (the
// client went away).
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// WriteOK writes an RPC success body {"ok":true,"result":v} with 200.
func WriteOK(w http.ResponseWriter, v any) {
	if v == nil {
		v = protocol.Empty{}
	}
	WriteJSON(w, http.StatusOK, protocol.RPCResponse[any]{OK: true, Result: v})
}

// NoContent writes 204.
func NoContent(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }

// DecodeJSON decodes the request body into v. An empty body decodes as {}.
// Bodies larger than MaxJSONBody, malformed JSON and trailing data yield a
// 400 invalid_request *Error. Unknown fields are accepted (forward
// compatibility).
func DecodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	body := http.MaxBytesReader(w, r.Body, MaxJSONBody)
	data, err := io.ReadAll(body)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return BadRequest(fmt.Sprintf("request body larger than %d bytes", MaxJSONBody))
		}
		return BadRequest("could not read the request body").Wrap(err)
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		data = []byte("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(v); err != nil {
		return BadRequest("invalid JSON: " + err.Error()).Wrap(err)
	}
	if dec.More() {
		return BadRequest("invalid JSON: trailing data")
	}
	return nil
}
