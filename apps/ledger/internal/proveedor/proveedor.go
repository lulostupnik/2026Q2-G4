package proveedor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	EstadoAcreditado = "acreditado"
	EstadoRechazado  = "rechazado"
	EstadoPendiente  = "pendiente"
)

type Error struct {
	Codigo  int
	Cuerpo  string
	Rechazo bool
}

func (e *Error) Error() string {
	if e.Rechazo {
		return fmt.Sprintf("el proveedor rechazo el pedido con %d: %s", e.Codigo, e.Cuerpo)
	}
	return fmt.Sprintf("el proveedor respondio %d", e.Codigo)
}

func EsRechazo(err error) (*Error, bool) {
	var pe *Error
	if errors.As(err, &pe) && pe.Rechazo {
		return pe, true
	}
	return nil, false
}

func EsCredencial(err error) bool {
	pe, ok := EsRechazo(err)
	return ok && (pe.Codigo == http.StatusUnauthorized || pe.Codigo == http.StatusForbidden)
}

type Cliente struct {
	base  string
	http  *http.Client
	clave string

	resolver func(context.Context) string
}

func (c *Cliente) ConResolver(f func(context.Context) string) { c.resolver = f }

func (c *Cliente) baseVigente(ctx context.Context) string {
	if c.resolver != nil {
		if b := c.resolver(ctx); b != "" {
			return b
		}
	}
	return c.base
}

func NuevoConClave(base string, timeout time.Duration, clave string) *Cliente {
	c := Nuevo(base, timeout)
	c.clave = clave
	return c
}

func Nuevo(base string, timeout time.Duration) *Cliente {
	return &Cliente{base: base, http: &http.Client{Timeout: timeout}}
}

type RespuestaEgreso struct {
	Referencia string `json:"referencia"`
	Estado     string `json:"estado"`
	Motivo     string `json:"motivo,omitempty"`
}

type Movimiento struct {
	IDOrigen      string    `json:"id_origen"`
	Referencia    string    `json:"referencia"`
	MontoCentavos int64     `json:"monto_centavos"`
	OcurridoEn    time.Time `json:"ocurrido_en"`
}

type respuestaMovimientos struct {
	Movimientos []Movimiento `json:"movimientos"`
	Cursor      string       `json:"cursor"`
}

func (c *Cliente) OrdenarEgreso(ctx context.Context, referencia, cbu string, montoCentavos int64) (RespuestaEgreso, error) {
	cuerpo, _ := json.Marshal(map[string]any{
		"referencia":     referencia,
		"cbu":            cbu,
		"monto_centavos": montoCentavos,
	})
	var out RespuestaEgreso
	err := c.pedir(ctx, http.MethodPost, "/egresos", cuerpo, &out)
	return out, err
}

func (c *Cliente) ConsultarEgreso(ctx context.Context, referencia string) (RespuestaEgreso, error) {
	var out RespuestaEgreso
	err := c.pedir(ctx, http.MethodGet, "/egresos/"+url.PathEscape(referencia), nil, &out)
	return out, err
}

func (c *Cliente) Movimientos(ctx context.Context, cursor string) (movs []Movimiento, siguiente string, crudo []byte, err error) {
	ruta := "/movimientos"
	if cursor != "" {
		ruta += "?desde=" + url.QueryEscape(cursor)
	}
	crudo, err = c.pedirCrudo(ctx, http.MethodGet, ruta, nil)
	if err != nil {
		return nil, "", nil, err
	}
	var out respuestaMovimientos
	if err := json.Unmarshal(crudo, &out); err != nil {
		return nil, "", crudo, fmt.Errorf("respuesta del proveedor ilegible: %w", err)
	}
	return out.Movimientos, out.Cursor, crudo, nil
}

func (c *Cliente) RegistrarDeposito(ctx context.Context, idOrigen, referencia string, montoCentavos int64) (Movimiento, error) {
	cuerpo, _ := json.Marshal(map[string]any{
		"id_origen":      idOrigen,
		"referencia":     referencia,
		"monto_centavos": montoCentavos,
	})
	var out Movimiento
	err := c.pedir(ctx, http.MethodPost, "/depositos", cuerpo, &out)
	return out, err
}

func (c *Cliente) pedir(ctx context.Context, metodo, ruta string, cuerpo []byte, destino any) error {
	crudo, err := c.pedirCrudo(ctx, metodo, ruta, cuerpo)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(crudo, destino); err != nil {
		return fmt.Errorf("respuesta del proveedor ilegible: %w", err)
	}
	return nil
}

func (c *Cliente) pedirCrudo(ctx context.Context, metodo, ruta string, cuerpo []byte) ([]byte, error) {

	var lector io.Reader
	if len(cuerpo) > 0 {
		lector = bytes.NewReader(cuerpo)
	}
	req, err := http.NewRequestWithContext(ctx, metodo, c.baseVigente(ctx)+ruta, lector)
	if err != nil {
		return nil, err
	}
	if lector != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.clave != "" {
		req.Header.Set("X-Mock-Key", c.clave)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llamar al proveedor: %w", err)
	}
	defer resp.Body.Close()

	crudo, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("leer respuesta del proveedor: %w", err)
	}
	if resp.StatusCode >= 500 {

		return nil, &Error{Codigo: resp.StatusCode, Cuerpo: string(crudo)}
	}
	if resp.StatusCode >= 400 {

		return nil, &Error{Codigo: resp.StatusCode, Cuerpo: string(crudo), Rechazo: true}
	}
	return crudo, nil
}
