package bootstrap

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"remlink/internal/protocol"
)

const maxRequestBody = 1 << 20

// Handler exposes the exact v1 public Bootstrap routes.
func Handler(service *Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/server/info", func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(writer, http.StatusOK, service.ServerInfo())
	})
	mux.HandleFunc("POST /api/v1/bootstrap/register", func(writer http.ResponseWriter, request *http.Request) {
		var input RegisterRequest
		if err := decodeJSON(writer, request, &input); err != nil {
			writeAPIError(writer, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
			return
		}
		output, err := service.Register(request.Context(), input)
		if err != nil {
			switch {
			case errors.Is(err, ErrJoinTokenInvalid):
				writeAPIError(writer, http.StatusUnauthorized, string(protocol.ErrorJoinTokenInvalid), err.Error())
			case errors.Is(err, ErrNodeConflict):
				writeAPIError(writer, http.StatusConflict, "NODE_CONFLICT", err.Error())
			default:
				writeAPIError(writer, http.StatusBadRequest, "REGISTRATION_FAILED", err.Error())
			}
			return
		}
		writeJSON(writer, http.StatusCreated, output)
	})
	mux.HandleFunc("POST /api/v1/bootstrap/config", func(writer http.ResponseWriter, request *http.Request) {
		var input ConfigRequest
		if err := decodeJSON(writer, request, &input); err != nil {
			writeAPIError(writer, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
			return
		}
		output, err := service.Config(request.Context(), input)
		if err != nil {
			if errors.Is(err, ErrNodeAuthFailed) {
				writeAPIError(writer, http.StatusUnauthorized, string(protocol.ErrorNodeAuthFailed), err.Error())
				return
			}
			writeAPIError(writer, http.StatusInternalServerError, "CONFIG_FAILED", err.Error())
			return
		}
		writeJSON(writer, http.StatusOK, output)
	})
	return securityHeaders(mux)
}

func decodeJSON(writer http.ResponseWriter, request *http.Request, destination any) error {
	if contentType := request.Header.Get("Content-Type"); contentType != "" &&
		!strings.HasPrefix(strings.ToLower(contentType), "application/json") {
		return errors.New("Content-Type must be application/json")
	}
	request.Body = http.MaxBytesReader(writer, request.Body, maxRequestBody)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}

type apiErrorEnvelope struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeAPIError(writer http.ResponseWriter, status int, code, message string) {
	writeJSONStatus(writer, status, apiErrorEnvelope{Error: apiError{Code: code, Message: message}})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writeJSONStatus(writer, status, value)
}

func writeJSONStatus(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(writer, request)
	})
}
