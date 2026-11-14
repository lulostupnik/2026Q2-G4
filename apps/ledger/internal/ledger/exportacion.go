package ledger

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"ledger/internal/apierr"
	"ledger/internal/objetos"
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

	s.despertarExportaciones()
	return s.VerExportacion(ctx, id)
}

const (
	leaseExportacion  = 15 * time.Minute
	vigenciaDescarga  = 15 * time.Minute
	esperaExportacion = time.Minute
)

func (s *Servicio) despertarExportaciones() {
	select {
	case s.exportaciones <- struct{}{}:
	default:
	}
}

func (s *Servicio) AtenderExportaciones(ctx context.Context) {
	tic := time.NewTicker(esperaExportacion)
	defer tic.Stop()
	for {
		for {
			hubo, err := s.ResolverExportacionPendiente(ctx)
			if err != nil {
				slog.Error("resolver exportacion", "error", err)
			}
			if !hubo || ctx.Err() != nil {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tic.C:
		case <-s.exportaciones:
		}
	}
}

func (s *Servicio) ResolverExportacionPendiente(ctx context.Context) (bool, error) {
	var id int64
	var cuenta *int64
	var desde, hasta time.Time
	err := s.pool.QueryRow(ctx, `
		UPDATE solicitudes_exportacion SET tomada_en = now()
		WHERE id = (
			SELECT id FROM solicitudes_exportacion
			WHERE estado = $1 AND (tomada_en IS NULL OR tomada_en < now() - make_interval(secs => $2))
			ORDER BY id
			LIMIT 1
			FOR UPDATE SKIP LOCKED)
		RETURNING id, cuenta_id, desde, hasta`,
		ExportPendiente, leaseExportacion.Seconds()).Scan(&id, &cuenta, &desde, &hasta)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	periodos := []time.Time{}
	for p := desde; !p.After(hasta); p = p.AddDate(0, 1, 0) {
		periodos = append(periodos, p)
	}

	res, err := s.resolverExportacion(ctx, id, cuenta, periodos)
	if err != nil && ctx.Err() != nil {
		return true, err
	}
	if err != nil {
		_, errAnotar := s.pool.Exec(context.WithoutCancel(ctx), `
			UPDATE solicitudes_exportacion
			SET estado = $2, motivo = $3, resuelta_en = now() WHERE id = $1`,
			id, ExportFallida, err.Error())
		return true, errors.Join(fmt.Errorf("exportacion %d: %w", id, err), errAnotar)
	}
	s.avisarExportacionLista(ctx, res)
	return true, nil
}

func (s *Servicio) avisarExportacionLista(ctx context.Context, e *Exportacion) {
	if !s.avisos.Habilitado() || e.CuentaID == nil {
		return
	}
	var titular *string
	if err := s.pool.QueryRow(ctx, `SELECT titular FROM cuentas WHERE id = $1`, *e.CuentaID).Scan(&titular); err != nil {
		slog.Error("buscar el titular para avisar la exportacion", "exportacion", e.ID, "error", err)
		return
	}
	if titular == nil {
		return
	}
	mensaje := fmt.Sprintf("Tu exportación N.º %d (%s a %s, %d movimientos) está lista.\n\n"+
		"Para descargarla, entrá a la app: %s\n\nEl archivo queda disponible unos días.",
		e.ID, e.Desde, e.Hasta, valor(e.Filas), s.appURL)
	if err := s.avisos.Avisar(ctx, *titular, "Tu exportación está lista", mensaje); err != nil {
		slog.Error("avisar la exportacion", "exportacion", e.ID, "error", err)
		return
	}
	slog.Info("exportacion avisada", "exportacion", e.ID)
}

func valor(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

type Descarga struct {
	URL     string    `json:"url"`
	VenceEn time.Time `json:"vence_en"`
}

func (s *Servicio) DescargarExportacion(ctx context.Context, e *Exportacion) (*Descarga, error) {
	if e.Estado != ExportLista || e.Objeto == "" {
		return nil, apierr.New(http.StatusConflict, "exportacion_no_lista",
			"la exportacion %d esta %s: se puede descargar cuando este lista", e.ID, e.Estado)
	}
	url, err := s.obj.URLDescarga(ctx, e.Objeto, fmt.Sprintf("exportacion-%d.ndjson.gz", e.ID), vigenciaDescarga)
	if errors.Is(err, objetos.ErrNoExiste) {
		return nil, apierr.New(http.StatusGone, "exportacion_vencida",
			"el archivo de la exportacion %d ya no esta: pedila de nuevo", e.ID)
	}
	if err != nil {
		return nil, err
	}
	return &Descarga{URL: url, VenceEn: time.Now().Add(vigenciaDescarga).UTC()}, nil
}

func (s *Servicio) resolverExportacion(ctx context.Context, id int64, cuenta *int64, periodos []time.Time) (*Exportacion, error) {
	var filas int64
	var deBase, deArchivo int

	clave := fmt.Sprintf("%ssolicitud=%d/asientos.ndjson.gz", prefijoExportaciones, id)
	subido, err := s.obj.Subir(ctx, clave, "application/gzip", func(w io.Writer) error {
		comprimido := gzip.NewWriter(w)
		codificador := json.NewEncoder(comprimido)
		for _, p := range periodos {
			origen, _, err := s.recorrerPeriodo(ctx, cuenta, p, func(m Movimiento) error {
				filas++
				return codificador.Encode(m)
			})
			if err != nil {
				return err
			}
			if origen == OrigenBase {
				deBase++
			} else {
				deArchivo++
			}
		}
		return comprimido.Close()
	})
	if err != nil {
		return nil, err
	}

	if _, err := s.pool.Exec(ctx, `
		UPDATE solicitudes_exportacion
		SET estado = $2, clave = $3, uri = $4, filas = $5,
		    periodos_base = $6, periodos_archivo = $7, resuelta_en = now()
		WHERE id = $1`, id, ExportLista, clave, subido.URI, filas, deBase, deArchivo); err != nil {
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
