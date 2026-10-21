package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"ledger/internal/apierr"
	"ledger/internal/ledger"
	"ledger/internal/sesion"
)

type claveSesion struct{}

type Sesion struct {
	Sub      string
	CuentaID int64
	Alias    string
	Expira   time.Time
}

func (a *API) verSesion(w http.ResponseWriter, r *http.Request) {
	s, ok := sesionDe(r)
	if !ok {
		apierr.Escribir(w, http.StatusOK, map[string]any{"sesion": false, "auth_abierta": true})
		return
	}
	cuerpo := map[string]any{
		"sesion":    true,
		"sub":       s.Sub,
		"expira_en": s.Expira,
		"cuenta_id": nil,
	}
	if s.CuentaID != 0 {
		cuerpo["cuenta_id"] = s.CuentaID
		cuerpo["alias"] = s.Alias
	} else {
		cuerpo["_links"] = Enlaces{"crear_cuenta": "/cuentas"}
	}
	apierr.Escribir(w, http.StatusOK, cuerpo)
}

func (a *API) conIdentidad(h http.HandlerFunc) http.HandlerFunc {
	if !a.sesiones.Habilitado() {
		return h
	}
	return func(w http.ResponseWriter, r *http.Request) {
		crudo := sesion.DelEncabezado(r.Header.Get("Authorization"))
		if crudo == "" {
			apierr.EscribirError(w, apierr.New(http.StatusUnauthorized, "sin_sesion",
				"falta la cabecera Authorization: Bearer <access token de Cognito>"))
			return
		}
		id, err := a.sesiones.Validar(r.Context(), crudo)
		switch {
		case errors.Is(err, sesion.ErrVencido):
			apierr.EscribirError(w, apierr.New(http.StatusUnauthorized, "sesion_vencida",
				"el token vencio: volve a iniciar sesion"))
			return
		case errors.Is(err, sesion.ErrInvalido):
			apierr.EscribirError(w, apierr.New(http.StatusUnauthorized, "sesion_invalida",
				"el token no es valido"))
			return
		case err != nil:
			slog.Error("no se pudieron leer las claves de Cognito", "error", err)
			apierr.EscribirError(w, apierr.New(http.StatusServiceUnavailable, "cognito_no_disponible",
				"no se pudieron leer las claves de Cognito para validar el token"))
			return
		}

		s := Sesion{Sub: id.Sub, Expira: id.Expira}
		c, err := a.svc.CuentaDeTitular(r.Context(), id.Sub)
		if err != nil {
			apierr.EscribirError(w, err)
			return
		}
		if c != nil {
			s.CuentaID, s.Alias = c.ID, c.Alias
		}
		h(w, r.WithContext(context.WithValue(r.Context(), claveSesion{}, s)))
	}
}

func (a *API) conSesion(h http.HandlerFunc) http.HandlerFunc {
	return a.conIdentidad(func(w http.ResponseWriter, r *http.Request) {
		if s, ok := sesionDe(r); ok && s.CuentaID == 0 {
			apierr.EscribirError(w, apierr.New(http.StatusForbidden, "sin_cuenta",
				"el usuario todavia no tiene cuenta: creala en POST /cuentas"))
			return
		}
		h(w, r)
	})
}

func sesionDe(r *http.Request) (Sesion, bool) {
	s, ok := r.Context().Value(claveSesion{}).(Sesion)
	return s, ok
}

func titular(r *http.Request) int64 {
	if s, ok := sesionDe(r); ok {
		return s.CuentaID
	}
	return 0
}

func (a *API) exigirTitular(w http.ResponseWriter, r *http.Request, cuentaID int64) bool {
	mia := titular(r)
	if mia == 0 || mia == cuentaID {
		return true
	}
	apierr.EscribirError(w, apierr.New(http.StatusForbidden, "cuenta_ajena",
		"la sesion es de la cuenta %d y el pedido es sobre la %d", mia, cuentaID))
	return false
}

func (a *API) exigirParteDeLaOrden(w http.ResponseWriter, r *http.Request, o *ledger.Orden) bool {
	mia := titular(r)
	if mia == 0 || o == nil {
		return true
	}
	if (o.OrigenID != nil && *o.OrigenID == mia) || (o.DestinoID != nil && *o.DestinoID == mia) {
		return true
	}
	apierr.EscribirError(w, apierr.New(http.StatusForbidden, "orden_ajena",
		"la orden %d no involucra a la cuenta %d", o.ID, mia))
	return false
}
