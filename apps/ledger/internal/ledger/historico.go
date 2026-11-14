package ledger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"ledger/internal/apierr"
	"ledger/internal/archivado"
	"ledger/internal/objetos"
	"ledger/internal/pg"
)

const (
	OrigenBase    = "base"
	OrigenArchivo = "archivo"
)

type ExtractoPeriodo struct {
	CuentaID    int64        `json:"cuenta_id"`
	Periodo     string       `json:"periodo"`
	Origen      string       `json:"origen"`
	Objeto      string       `json:"objeto,omitempty"`
	Movimientos []Movimiento `json:"movimientos"`
}

type fuentePeriodo struct {
	Movimientos []Movimiento
	Origen      string
	Clave       string
}

func (s *Servicio) asientosDePeriodo(ctx context.Context, cuenta *int64, periodo time.Time) (fuentePeriodo, error) {
	fuente := fuentePeriodo{Movimientos: []Movimiento{}}
	origen, clave, err := s.recorrerPeriodo(ctx, cuenta, periodo, func(m Movimiento) error {
		fuente.Movimientos = append(fuente.Movimientos, m)
		return nil
	})
	fuente.Origen, fuente.Clave = origen, clave
	return fuente, err
}

func (s *Servicio) recorrerPeriodo(ctx context.Context, cuenta *int64, periodo time.Time, fn func(Movimiento) error) (string, string, error) {
	etiqueta := periodo.Format("2006-01")

	var clave *string
	var filas *int64
	err := s.pool.QueryRow(ctx,
		`SELECT clave, filas FROM periodos_archivados WHERE periodo = $1`, periodo).Scan(&clave, &filas)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return OrigenBase, "", s.recorrerBase(ctx, cuenta, periodo, fn)
	case err != nil:
		return "", "", err
	}

	if clave == nil {
		return "", "", apierr.New(http.StatusServiceUnavailable, "archivado_en_curso",
			"el periodo %s se esta archivando: reintentá en unos minutos", etiqueta)
	}
	if filas != nil && *filas == 0 {
		return OrigenArchivo, *clave, nil
	}

	partes, err := s.partesDelArchivo(ctx, periodo, cuenta)
	if err != nil {
		return "", "", err
	}
	objeto := *clave
	if cuenta != nil && len(partes) == 1 {
		objeto = partes[0].clave
	}

	for _, p := range partes {
		lectura, err := s.lector.Recorrer(ctx, p.clave, p.bytes, cuenta, func(f archivado.Fila) error {
			return fn(Movimiento{
				Periodo:       periodo,
				CuentaID:      f.CuentaID,
				OrdenID:       f.OrdenID,
				ContraparteID: f.ContraparteID,
				MontoCentavos: f.MontoCentavos,
				Tramo:         f.Tramo,
				CreadoEn:      f.CreadoEn,
			})
		})
		if errors.Is(err, objetos.ErrNoExiste) {
			return "", "", fmt.Errorf("el periodo %s figura archivado en %s pero el objeto no esta", etiqueta, p.clave)
		}
		if err != nil {
			return "", "", err
		}
		slog.Info("lectura del archivo", "objeto", p.clave, "bytes", p.bytes,
			"bloques", lectura.Bloques, "bloques_leidos", lectura.BloquesLeidos, "pedidos_s3", lectura.Pedidos,
			"bytes_leidos", lectura.Bytes)
	}
	return OrigenArchivo, objeto, nil
}

type parteArchivo struct {
	clave string
	bytes int64
}

func (s *Servicio) partesDelArchivo(ctx context.Context, periodo time.Time, cuenta *int64) ([]parteArchivo, error) {
	filas, err := s.pool.Query(ctx, `
		SELECT clave, bytes FROM archivo_partes
		WHERE periodo = $1 AND ($2::bigint IS NULL OR $2 BETWEEN cuenta_min AND cuenta_max)
		ORDER BY parte`, periodo, cuenta)
	if err != nil {
		return nil, err
	}
	defer filas.Close()

	var partes []parteArchivo
	for filas.Next() {
		var p parteArchivo
		if err := filas.Scan(&p.clave, &p.bytes); err != nil {
			return nil, err
		}
		partes = append(partes, p)
	}
	return partes, filas.Err()
}

func (s *Servicio) MovimientosDePeriodo(ctx context.Context, cuentaID int64, periodo time.Time) (*ExtractoPeriodo, error) {
	periodo = pg.InicioDeMes(periodo)

	var existe bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM cuentas WHERE id = $1)`, cuentaID).Scan(&existe); err != nil {
		return nil, err
	}
	if !existe {
		return nil, apierr.NoEncontrado("no existe la cuenta %d", cuentaID)
	}

	fuente, err := s.asientosDePeriodo(ctx, &cuentaID, periodo)
	if err != nil {
		return nil, err
	}
	return &ExtractoPeriodo{
		CuentaID:    cuentaID,
		Periodo:     periodo.Format("2006-01"),
		Origen:      fuente.Origen,
		Objeto:      fuente.Clave,
		Movimientos: fuente.Movimientos,
	}, nil
}

func (s *Servicio) recorrerBase(ctx context.Context, cuenta *int64, periodo time.Time, fn func(Movimiento) error) error {
	filas, err := s.pool.Query(ctx, `
		SELECT periodo, cuenta_id, orden_id, contraparte_id, monto_centavos, tramo, creado_en
		FROM asientos
		WHERE periodo = $1 AND ($2::bigint IS NULL OR cuenta_id = $2)
		ORDER BY cuenta_id, creado_en, id`, periodo, cuenta)
	if err != nil {
		return err
	}
	defer filas.Close()

	for filas.Next() {
		var m Movimiento
		if err := filas.Scan(&m.Periodo, &m.CuentaID, &m.OrdenID, &m.ContraparteID, &m.MontoCentavos, &m.Tramo, &m.CreadoEn); err != nil {
			return err
		}
		if err := fn(m); err != nil {
			return err
		}
	}
	return filas.Err()
}
