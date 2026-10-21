package ledger

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"ledger/internal/apierr"
	"ledger/internal/pg"
)

const (
	ExportPendiente = "pendiente"
	ExportLista     = "lista"
	ExportFallida   = "fallida"
)

const prefijoExportaciones = "exports/"

const maxPeriodosExportables = 24

type EntradaExportacion struct {
	CuentaID int64  `json:"cuenta_id,omitempty"`
	Desde    string `json:"desde"`
	Hasta    string `json:"hasta"`
}

type Exportacion struct {
	ID              int64      `json:"id"`
	CuentaID        *int64     `json:"cuenta_id,omitempty"`
	Desde           string     `json:"desde"`
	Hasta           string     `json:"hasta"`
	Estado          string     `json:"estado"`
	Objeto          string     `json:"objeto,omitempty"`
	URI             string     `json:"uri,omitempty"`
	Filas           *int64     `json:"filas,omitempty"`
	PeriodosBase    *int       `json:"periodos_base,omitempty"`
	PeriodosArchivo *int       `json:"periodos_archivo,omitempty"`
	Motivo          string     `json:"motivo,omitempty"`
	SolicitadaEn    time.Time  `json:"solicitada_en"`
	ResueltaEn      *time.Time `json:"resuelta_en,omitempty"`
}

func (s *Servicio) Exportar(ctx context.Context, e EntradaExportacion) (*Exportacion, error) {
	desde, err := time.Parse("2006-01", e.Desde)
	if err != nil {
		return nil, apierr.Invalido("desde tiene que tener formato AAAA-MM")
	}
	hasta, err := time.Parse("2006-01", e.Hasta)
	if err != nil {
		return nil, apierr.Invalido("hasta tiene que tener formato AAAA-MM")
	}
	desde, hasta = pg.InicioDeMes(desde), pg.InicioDeMes(hasta)
	if hasta.Before(desde) {
		return nil, apierr.Invalido("hasta no puede ser anterior a desde")
	}

	periodos := []time.Time{}
	for p := desde; !p.After(hasta); p = p.AddDate(0, 1, 0) {
		periodos = append(periodos, p)
	}
	if len(periodos) > maxPeriodosExportables {
		return nil, apierr.Invalido("el rango es de %d meses y el maximo es %d: pedilo en tramos",
			len(periodos), maxPeriodosExportables)
	}

	var cuenta *int64
	if e.CuentaID != 0 {
		var existe bool
		if err := s.pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM cuentas WHERE id = $1)`, e.CuentaID).Scan(&existe); err != nil {
			return nil, err
		}
		if !existe {
			return nil, apierr.NoEncontrado("no existe la cuenta %d", e.CuentaID)
		}
		cuenta = &e.CuentaID
	}

	var id int64
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO solicitudes_exportacion (cuenta_id, desde, hasta, estado)
		VALUES ($1, $2, $3, $4) RETURNING id`,
		cuenta, desde, hasta, ExportPendiente).Scan(&id); err != nil {
		return nil, err
	}

	res, err := s.resolverExportacion(ctx, id, cuenta, periodos)
	if err != nil {

		_, errAnotar := s.pool.Exec(context.WithoutCancel(ctx), `
			UPDATE solicitudes_exportacion
			SET estado = $2, motivo = $3, resuelta_en = now() WHERE id = $1`,
			id, ExportFallida, err.Error())
		if errAnotar != nil {
			return nil, errors.Join(err, errAnotar)
		}
		return nil, err
	}
	return res, nil
}

func (s *Servicio) resolverExportacion(ctx context.Context, id int64, cuenta *int64, periodos []time.Time) (*Exportacion, error) {
	var cuerpo bytes.Buffer
	comprimido := gzip.NewWriter(&cuerpo)
	codificador := json.NewEncoder(comprimido)

	var filas int64
	var deBase, deArchivo int

	for _, p := range periodos {
		fuente, err := s.asientosDePeriodo(ctx, cuenta, p)
		if err != nil {
			return nil, err
		}
		if fuente.Origen == OrigenBase {
			deBase++
		} else {
			deArchivo++
		}
		for _, m := range fuente.Movimientos {
			if err := codificador.Encode(m); err != nil {
				return nil, err
			}
			filas++
		}
	}

	if err := comprimido.Close(); err != nil {
		return nil, err
	}

	clave := fmt.Sprintf("%ssolicitud=%d/asientos.ndjson.gz", prefijoExportaciones, id)
	uri, err := s.obj.Guardar(ctx, clave, cuerpo.Bytes(), "application/gzip")
	if err != nil {
		return nil, err
	}

	if _, err := s.pool.Exec(ctx, `
		UPDATE solicitudes_exportacion
		SET estado = $2, clave = $3, uri = $4, filas = $5,
		    periodos_base = $6, periodos_archivo = $7, resuelta_en = now()
		WHERE id = $1`, id, ExportLista, clave, uri, filas, deBase, deArchivo); err != nil {
		return nil, err
	}
	return s.VerExportacion(ctx, id)
}

func (s *Servicio) VerExportacion(ctx context.Context, id int64) (*Exportacion, error) {
	var e Exportacion
	var desde, hasta time.Time
	var clave, uri, motivo *string
	err := s.pool.QueryRow(ctx, `
		SELECT id, cuenta_id, desde, hasta, estado, clave, uri, filas,
		       periodos_base, periodos_archivo, motivo, solicitada_en, resuelta_en
		FROM solicitudes_exportacion WHERE id = $1`, id).Scan(
		&e.ID, &e.CuentaID, &desde, &hasta, &e.Estado, &clave, &uri, &e.Filas,
		&e.PeriodosBase, &e.PeriodosArchivo, &motivo, &e.SolicitadaEn, &e.ResueltaEn)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierr.NoEncontrado("no existe la exportacion %d", id)
	}
	if err != nil {
		return nil, err
	}

	e.Desde, e.Hasta = desde.Format("2006-01"), hasta.Format("2006-01")
	if clave != nil {
		e.Objeto = *clave
	}
	if uri != nil {
		e.URI = *uri
	}
	if motivo != nil {
		e.Motivo = *motivo
	}
	return &e, nil
}
