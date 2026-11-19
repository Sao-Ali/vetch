package workload

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

const MaxBodyBytes = 1 << 20

func Decode(writer http.ResponseWriter, request *http.Request, value any) error {
	request.Body = http.MaxBytesReader(writer, request.Body, MaxBodyBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("body must contain one JSON object")
	}
	return nil
}

func Write(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func Fail(writer http.ResponseWriter, status int, code, message string) {
	Write(writer, status, Failure{Code: code, Message: message})
}

func (request Request) Validate() error {
	if strings.TrimSpace(request.ID) == "" || len(request.ID) > 128 || strings.TrimSpace(request.Model) == "" {
		return errors.New("id (at most 128 characters) and model are required")
	}
	if strings.TrimSpace(request.Prompt) == "" || len(request.Prompt) > 65536 {
		return errors.New("prompt must contain between 1 and 65536 bytes")
	}
	if request.MaxTokens < 1 || request.MaxTokens > 128 {
		return errors.New("maxTokens must be between 1 and 128")
	}
	remaining := time.Until(request.Deadline)
	if remaining <= 0 || remaining > 2*time.Minute {
		return errors.New("deadline must be in the next two minutes")
	}
	return nil
}
