package mock

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	centavosRechazo      = 13
	centavosSinRespuesta = 99
)

const Prefijo = "/servicio-mock"

const CabeceraClave = "X-Mock-Key"

type Opciones struct {
	Clave string

	DemoraSinRespuesta time.Duration
}

type Servicio struct {
	pool *pgxpool.Pool
	op   Opciones
}

func Nuevo(pool *pgxpool.Pool, op Opciones) *Servicio {
	if op.DemoraSinRespuesta <= 0 {
		op.DemoraSinRespuesta = 30 * time.Second
	}
	return &Servicio{pool: pool, op: op}
}

func (s *Servicio) autorizado(w http.ResponseWriter, r *http.Request) bool {
	if s.op.Clave == "" {
		return true
	}

	dada := r.Header.Get(CabeceraClave)
	if subtle.ConstantTimeCompare([]byte(dada), []byte(s.op.Clave)) != 1 {
		escribir(w, http.StatusUnauthorized, map[string]string{"error": "clave del mock invalida o ausente"})
		return false
	}
	return true
}

func (s *Servicio) protegido(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.autorizado(w, r) {
			return
		}
		h(w, r)
	}
}

type Egreso struct {
	Referencia    string `json:"referencia"`
	CBU           string `json:"cbu"`
	MontoCentavos int64  `json:"monto_centavos"`
	Estado        string `json:"estado"`
	Motivo        string `json:"motivo,omitempty"`
}

type Movimiento struct {
	IDOrigen      string    `json:"id_origen"`
	Referencia    string    `json:"referencia"`
	MontoCentavos int64     `json:"monto_centavos"`
	OcurridoEn    time.Time `json:"ocurrido_en"`
}

func (s *Servicio) Registrar(mux *http.ServeMux) {
	mux.HandleFunc("GET "+Prefijo+"/health", func(w http.ResponseWriter, _ *http.Request) {
		escribir(w, http.StatusOK, map[string]string{"estado": "ok", "servicio": "mock del proveedor"})
	})
	mux.HandleFunc("POST "+Prefijo+"/egresos", s.protegido(s.ordenarEgreso))
	mux.HandleFunc("GET "+Prefijo+"/egresos/{referencia}", s.protegido(s.consultarEgreso))
	mux.HandleFunc("GET "+Prefijo+"/movimientos", s.protegido(s.listarMovimientos))
	mux.HandleFunc("POST "+Prefijo+"/depositos", s.protegido(s.registrarDeposito))
	mux.HandleFunc("GET "+Prefijo+"/pedidos", s.protegido(s.listarPedidos))
}

func (s *Servicio) listarPedidos(w http.ResponseWriter, r *http.Request) {
	limite, _ := strconv.Atoi(r.URL.Query().Get("limite"))
	if limite <= 0 || limite > 200 {
		limite = 50
	}

	filas, err := s.pool.Query(r.Context(), `
		SELECT referencia, cbu, monto_centavos, estado, coalesce(motivo, ''), muda, creado_en
		FROM mock_egresos ORDER BY creado_en DESC LIMIT $1`, limite)
	if err != nil {
		escribir(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer filas.Close()

	type pedido struct {
		Referencia    string `json:"referencia"`
		CBU           string `json:"cbu"`
		MontoCentavos int64  `json:"monto_centavos"`
		Estado        string `json:"estado"`
		Motivo        string `json:"motivo,omitempty"`

		Muda     bool      `json:"sin_respuesta"`
		CreadoEn time.Time `json:"creado_en"`
	}

	pedidos := []pedido{}
	for filas.Next() {
		var p pedido
		if err := filas.Scan(&p.Referencia, &p.CBU, &p.MontoCentavos, &p.Estado,
			&p.Motivo, &p.Muda, &p.CreadoEn); err != nil {
			escribir(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		pedidos = append(pedidos, p)
	}
	if err := filas.Err(); err != nil {
		escribir(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	escribir(w, http.StatusOK, map[string]any{"pedidos": pedidos})
}

func (s *Servicio) ordenarEgreso(w http.ResponseWriter, r *http.Request) {
	var e Egreso
	if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if e.Referencia == "" {
		escribir(w, http.StatusBadRequest, map[string]string{"error": "falta referencia"})
		return
	}

	estado, motivo := "acreditado", ""
	muda := false
	switch e.MontoCentavos % 100 {
	case centavosRechazo:
		estado, motivo = "rechazado", "cuenta de destino inexistente"
	case centavosSinRespuesta:
		muda = true
	}

	if _, err := s.pool.Exec(r.Context(), `
		INSERT INTO mock_egresos (referencia, cbu, monto_centavos, estado, motivo, muda)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6)
		ON CONFLICT (referencia) DO NOTHING`,
		e.Referencia, e.CBU, e.MontoCentavos, estado, motivo, muda); err != nil {
		escribir(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	guardado, err := s.leer(r.Context(), e.Referencia)
	if err != nil {
		escribir(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if guardado.muda {

		slog.Warn("mock: egreso ejecutado sin responder",
			"referencia", e.Referencia, "demora", s.op.DemoraSinRespuesta)
		select {
		case <-time.After(s.op.DemoraSinRespuesta):
		case <-r.Context().Done():
		}
		return
	}
	escribir(w, http.StatusOK, guardado.Egreso)
}

func (s *Servicio) consultarEgreso(w http.ResponseWriter, r *http.Request) {
	ref := r.PathValue("referencia")

	guardado, err := s.leer(r.Context(), ref)
	if errors.Is(err, pgx.ErrNoRows) {

		escribir(w, http.StatusOK, Egreso{Referencia: ref, Estado: "rechazado", Motivo: "no registrado"})
		return
	}
	if err != nil {
		escribir(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	escribir(w, http.StatusOK, guardado.Egreso)
}

type egresoGuardado struct {
	Egreso
	muda bool
}

func (s *Servicio) leer(ctx context.Context, ref string) (*egresoGuardado, error) {
	var g egresoGuardado
	var motivo *string
	err := s.pool.QueryRow(ctx, `
		SELECT referencia, cbu, monto_centavos, estado, motivo, muda
		FROM mock_egresos WHERE referencia = $1`, ref).
		Scan(&g.Referencia, &g.CBU, &g.MontoCentavos, &g.Estado, &motivo, &g.muda)
	if err != nil {
		return nil, err
	}
	if motivo != nil {
		g.Motivo = *motivo
	}
	return &g, nil
}

func (s *Servicio) listarMovimientos(w http.ResponseWriter, r *http.Request) {
	desde, _ := strconv.ParseInt(r.URL.Query().Get("desde"), 10, 64)
	if desde < 0 {
		desde = 0
	}

	filas, err := s.pool.Query(r.Context(), `
		SELECT id, id_origen, coalesce(referencia, ''), monto_centavos, ocurrido_en
		FROM mock_movimientos WHERE id > $1 ORDER BY id LIMIT 500`, desde)
	if err != nil {
		escribir(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer filas.Close()

	movs := []Movimiento{}
	cursor := desde
	for filas.Next() {
		var m Movimiento
		var id int64
		if err := filas.Scan(&id, &m.IDOrigen, &m.Referencia, &m.MontoCentavos, &m.OcurridoEn); err != nil {
			escribir(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		movs = append(movs, m)
		cursor = id
	}
	if err := filas.Err(); err != nil {
		escribir(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	escribir(w, http.StatusOK, map[string]any{
		"movimientos": movs,
		"cursor":      strconv.FormatInt(cursor, 10),
	})
}

func (s *Servicio) registrarDeposito(w http.ResponseWriter, r *http.Request) {
	var m Movimiento
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if m.MontoCentavos <= 0 {
		escribir(w, http.StatusBadRequest, map[string]string{"error": "monto_centavos tiene que ser positivo"})
		return
	}
	if m.OcurridoEn.IsZero() {
		m.OcurridoEn = time.Now().UTC()
	}
	if m.IDOrigen == "" {
		m.IDOrigen = fmt.Sprintf("MOV-%d", time.Now().UnixNano())
	}

	var guardado Movimiento
	var referencia *string
	nuevo := true
	err := s.pool.QueryRow(r.Context(), `
		WITH insertado AS (
			INSERT INTO mock_movimientos (id_origen, referencia, monto_centavos, ocurrido_en)
			VALUES ($1, NULLIF($2, ''), $3, $4)
			ON CONFLICT (id_origen) DO NOTHING
			RETURNING id_origen, referencia, monto_centavos, ocurrido_en, TRUE AS nuevo
		)
		SELECT id_origen, referencia, monto_centavos, ocurrido_en, nuevo FROM insertado
		UNION ALL
		SELECT id_origen, referencia, monto_centavos, ocurrido_en, FALSE
		FROM mock_movimientos WHERE id_origen = $1
		LIMIT 1`,
		m.IDOrigen, m.Referencia, m.MontoCentavos, m.OcurridoEn).
		Scan(&guardado.IDOrigen, &referencia, &guardado.MontoCentavos, &guardado.OcurridoEn, &nuevo)
	if err != nil {
		escribir(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if referencia != nil {
		guardado.Referencia = *referencia
	}

	if !nuevo {

		escribir(w, http.StatusOK, guardado)
		return
	}
	escribir(w, http.StatusCreated, guardado)
}

func escribir(w http.ResponseWriter, estado int, cuerpo any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(estado)
	if err := json.NewEncoder(w).Encode(cuerpo); err != nil {
		slog.Error("mock: escribir respuesta", "error", err)
	}
}
