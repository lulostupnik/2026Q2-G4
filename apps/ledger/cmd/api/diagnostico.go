package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"

	"ledger/internal/apierr"
)

func huella(etiqueta, secreto string) string {
	if secreto == "" {
		return "(vacia)"
	}
	suma := sha256.Sum256([]byte("nucleo-ledger:huella:" + etiqueta + ":" + secreto))
	return hex.EncodeToString(suma[:])[:12]
}

func (a *API) diagnostico(w http.ResponseWriter, r *http.Request) {
	cuerpo := map[string]any{
		"instancia": instancia,
		"version":   version,
		"activo":    time.Since(arranque).Round(time.Second).String(),

		"huellas": map[string]any{
			"mock_api_key": map[string]string{
				"huella":  huella("mock", a.secretos.Mock),
				"igual_a": "el PROVEEDOR_API_KEY del OTRO equipo",
			},
			"proveedor_api_key": map[string]string{
				"huella":  huella("mock", a.secretos.Proveedor),
				"igual_a": "el MOCK_API_KEY del OTRO equipo",
			},
			"webhook_secret": map[string]string{
				"huella":  huella("webhook", a.secretos.Webhook),
				"igual_a": "el WEBHOOK_SECRET del OTRO equipo",
			},
			"admin_api_key": map[string]string{
				"huella":  huella("admin", a.secretos.Admin),
				"igual_a": "nadie: es propia de este despliegue",
			},
		},
		"sesiones_habilitadas": a.sesiones.Habilitado(),
	}

	if p, err := a.svc.VerProveedor(r.Context()); err == nil {
		cuerpo["proveedor"] = p
	}
	var cursor, proveedorDelCursor *string
	if err := a.pool.QueryRow(r.Context(),
		`SELECT ultimo_cursor, proveedor FROM conciliacion WHERE id = 1`).
		Scan(&cursor, &proveedorDelCursor); err == nil {
		cuerpo["conciliacion"] = map[string]any{
			"ultimo_cursor":        valor(cursor),
			"cursor_del_proveedor": valor(proveedorDelCursor),
			"que_significa":        "el cursor solo vale contra el proveedor que lo emitio; si no coinciden, la proxima conciliacion arranca de cero sola",
		}
	}

	apierr.Escribir(w, http.StatusOK, cuerpo)
}

func valor(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
