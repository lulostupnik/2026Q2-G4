package main

import (
	"fmt"
	"time"

	"ledger/internal/ledger"
)

type Enlaces map[string]string

type ordenConEnlaces struct {
	*ledger.Orden
	Links Enlaces `json:"_links"`
}

type cuentaConEnlaces struct {
	*ledger.CuentaDetalle
	Links Enlaces `json:"_links"`
}

type altaConEnlaces struct {
	*ledger.Cuenta
	Links Enlaces `json:"_links"`
}

type saldoConEnlaces struct {
	*ledger.Saldo
	Links Enlaces `json:"_links"`
}

type extractoConEnlaces struct {
	*ledger.ExtractoPeriodo
	Links Enlaces `json:"_links"`
}

type exportacionConEnlaces struct {
	*ledger.Exportacion
	Links Enlaces `json:"_links"`
}

func rutaTipo(tipo string) string {
	switch tipo {
	case ledger.TipoTransferencia:
		return "/transferencias"
	case ledger.TipoEgreso:
		return "/egresos"
	case ledger.TipoIngreso:
		return "/ingresos"
	default:
		return "/ordenes"
	}
}

func conEnlacesOrden(o *ledger.Orden) ordenConEnlaces {
	base := rutaTipo(o.Tipo)
	l := Enlaces{"self": fmt.Sprintf("%s/%d", base, o.ID)}

	if o.OrigenID != nil {
		l["origen"] = fmt.Sprintf("/cuentas/%d", *o.OrigenID)
		l["origen_saldo"] = fmt.Sprintf("/cuentas/%d/saldo", *o.OrigenID)
	}
	if o.DestinoID != nil {
		l["destino"] = fmt.Sprintf("/cuentas/%d", *o.DestinoID)
		l["destino_saldo"] = fmt.Sprintf("/cuentas/%d/saldo", *o.DestinoID)
	}

	switch o.Estado {
	case ledger.EstadoEnDuda:
		l["resolver"] = fmt.Sprintf("%s/%d", base, o.ID)
	case ledger.EstadoReservada, ledger.EstadoEnviada, ledger.EstadoPendiente:
		l["consultar"] = fmt.Sprintf("%s/%d", base, o.ID)
	}
	if o.Tipo == ledger.TipoIngreso && o.Referencia != "" && !ledger.EsTerminal(o.Estado) {
		l["conciliar"] = fmt.Sprintf("%s/%d", base, o.ID)

		l["simular_deposito"] = fmt.Sprintf("%s/%d/simular-deposito", base, o.ID)
	}

	return ordenConEnlaces{Orden: o, Links: l}
}

func conEnlacesCuenta(c *ledger.CuentaDetalle) cuentaConEnlaces {
	mes := time.Now().UTC().Format("2006-01")
	return cuentaConEnlaces{CuentaDetalle: c, Links: Enlaces{
		"self":            fmt.Sprintf("/cuentas/%d", c.ID),
		"saldo":           fmt.Sprintf("/cuentas/%d/saldo", c.ID),
		"movimientos":     fmt.Sprintf("/cuentas/%d/movimientos", c.ID),
		"extracto_actual": fmt.Sprintf("/cuentas/%d/movimientos?periodo=%s", c.ID, mes),
		"padron":          "/cuentas",
	}}
}

func conEnlacesAlta(c *ledger.Cuenta) altaConEnlaces {
	return altaConEnlaces{Cuenta: c, Links: Enlaces{
		"self":        fmt.Sprintf("/cuentas/%d", c.ID),
		"saldo":       fmt.Sprintf("/cuentas/%d/saldo", c.ID),
		"movimientos": fmt.Sprintf("/cuentas/%d/movimientos", c.ID),
	}}
}

func conEnlacesSaldo(s *ledger.Saldo) saldoConEnlaces {
	return saldoConEnlaces{Saldo: s, Links: Enlaces{
		"self":        fmt.Sprintf("/cuentas/%d/saldo", s.CuentaID),
		"cuenta":      fmt.Sprintf("/cuentas/%d", s.CuentaID),
		"movimientos": fmt.Sprintf("/cuentas/%d/movimientos", s.CuentaID),
	}}
}

func conEnlacesExtracto(e *ledger.ExtractoPeriodo) extractoConEnlaces {
	l := Enlaces{
		"self":   fmt.Sprintf("/cuentas/%d/movimientos?periodo=%s", e.CuentaID, e.Periodo),
		"cuenta": fmt.Sprintf("/cuentas/%d", e.CuentaID),
		"saldo":  fmt.Sprintf("/cuentas/%d/saldo", e.CuentaID),
	}
	if p, err := time.Parse("2006-01", e.Periodo); err == nil {
		l["periodo_anterior"] = fmt.Sprintf("/cuentas/%d/movimientos?periodo=%s",
			e.CuentaID, p.AddDate(0, -1, 0).Format("2006-01"))
		l["periodo_siguiente"] = fmt.Sprintf("/cuentas/%d/movimientos?periodo=%s",
			e.CuentaID, p.AddDate(0, 1, 0).Format("2006-01"))
		l["exportar"] = "/exportaciones"
	}
	return extractoConEnlaces{ExtractoPeriodo: e, Links: l}
}

func conEnlacesExportacion(e *ledger.Exportacion) exportacionConEnlaces {
	l := Enlaces{"self": fmt.Sprintf("/exportaciones/%d", e.ID)}
	if e.CuentaID != nil {
		l["cuenta"] = fmt.Sprintf("/cuentas/%d", *e.CuentaID)
	}
	if e.Estado == ledger.ExportLista && e.URI != "" {
		l["objeto"] = e.URI
	}
	return exportacionConEnlaces{Exportacion: e, Links: l}
}

func documentoRaiz() map[string]any {
	return map[string]any{
		"nombre":      "Núcleo Transaccional Elástico",
		"descripcion": "PSP con ledger de partida doble. Todo se resuelve dentro del request salvo el archivado.",
		"_links": Enlaces{
			"self":           "/",
			"documentacion":  "/docs",
			"contrato":       "/openapi.yaml",
			"salud":          "/health",
			"sesion_actual":  "/sesiones/actual",
			"cuentas":        "/cuentas",
			"transferencias": "/transferencias",
			"egresos":        "/egresos",
			"ingresos":       "/ingresos",
			"exportaciones":  "/exportaciones",
			"cierre":         "/admin/cierre",
			"archivado":      "/admin/archivado",
			"servicio_mock":  "/servicio-mock/health",
			"avisos":         "/webhooks/recibidos",
		},
		"idempotencia": map[string]string{
			"cabecera":  "Idempotency-Key",
			"requerida": "POST /transferencias, POST /egresos, POST /ingresos",
		},
		"sesion": map[string]string{
			"cabecera": "Authorization: Bearer <token>",
			"obtener":  "login en la Hosted UI de Cognito: el access token va en la cabecera",
			"alcance":  "cuentas, transferencias, egresos, ingresos y exportaciones",
		},
	}
}
