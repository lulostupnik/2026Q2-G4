package apierr

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
)

type Error struct {
	Estado  int    `json:"-"`
	Codigo  string `json:"codigo"`
	Mensaje string `json:"mensaje"`
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Codigo, e.Mensaje) }

func New(estado int, codigo, formato string, args ...any) *Error {
	return &Error{Estado: estado, Codigo: codigo, Mensaje: fmt.Sprintf(formato, args...)}
}

func Invalido(formato string, args ...any) *Error {
	return New(http.StatusBadRequest, "invalido", formato, args...)
}

func NoEncontrado(formato string, args ...any) *Error {
	return New(http.StatusNotFound, "no_encontrado", formato, args...)
}

func Rechazado(codigo, formato string, args ...any) *Error {
	return New(http.StatusUnprocessableEntity, codigo, formato, args...)
}

func Escribir(w http.ResponseWriter, estado int, cuerpo any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(estado)
	if cuerpo == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(cuerpo); err != nil {
		slog.Error("escribir respuesta", "error", err)
	}
}

func EscribirError(w http.ResponseWriter, err error) {
	var e *Error
	if errors.As(err, &e) {
		Escribir(w, e.Estado, e)
		return
	}
	slog.Error("error interno", "error", err)
	Escribir(w, http.StatusInternalServerError, &Error{
		Codigo:  "interno",
		Mensaje: "error interno",
	})
}
