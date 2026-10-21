package ledger

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	etiqueta := periodo.Format("2006-01")

	var clave *string
	var filas *int64
	err := s.pool.QueryRow(ctx,
		`SELECT clave, filas FROM periodos_archivados WHERE periodo = $1`, periodo).Scan(&clave, &filas)
	switch {
	case errors.Is(err, pgx.ErrNoRows):

		movs, err := s.asientosDeBase(ctx, cuenta, periodo)
		return fuentePeriodo{Movimientos: movs, Origen: OrigenBase}, err
	case err != nil:
		return fuentePeriodo{}, err
	}

	if clave == nil {
		return fuentePeriodo{}, apierr.New(http.StatusServiceUnavailable, "archivado_en_curso",
			"el periodo %s se esta archivando: reintentá en unos minutos", etiqueta)
	}

	fuente := fuentePeriodo{Movimientos: []Movimiento{}, Origen: OrigenArchivo, Clave: *clave}

	if filas != nil && *filas == 0 {
		return fuente, nil
	}

	movs, err := s.leerDelArchivo(ctx, *clave, cuenta, etiqueta)
	if err != nil {
		return fuentePeriodo{}, err
	}
	fuente.Movimientos = movs
	return fuente, nil
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

func (s *Servicio) leerDelArchivo(ctx context.Context, clave string, cuenta *int64, etiqueta string) ([]Movimiento, error) {
	crudo, err := s.obj.Leer(ctx, clave)
	if errors.Is(err, objetos.ErrNoExiste) {

		return nil, fmt.Errorf("el periodo %s figura archivado en %s pero el objeto no esta", etiqueta, clave)
	}
	if err != nil {
		return nil, err
	}

	descomprimido, err := gzip.NewReader(bytes.NewReader(crudo))
	if err != nil {
		return nil, fmt.Errorf("descomprimir %s: %w", clave, err)
	}
	defer descomprimido.Close()

	movs := []Movimiento{}
	lector := bufio.NewScanner(descomprimido)

	lector.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for lector.Scan() {
		linea := lector.Bytes()
		if len(linea) == 0 {
			continue
		}
		var a archivado.Asiento
		if err := json.Unmarshal(linea, &a); err != nil {
			return nil, fmt.Errorf("linea invalida en %s: %w", clave, err)
		}
		if cuenta != nil && a.CuentaID != *cuenta {
			continue
		}
		periodo, _ := time.Parse("2006-01-02", a.Periodo)
		movs = append(movs, Movimiento{
			Periodo:       periodo,
			CuentaID:      a.CuentaID,
			OrdenID:       a.OrdenID,
			ContraparteID: a.ContraparteID,
			MontoCentavos: a.MontoCentavos,
			Tramo:         a.Tramo,
			CreadoEn:      a.CreadoEn,
		})
	}
	return movs, lector.Err()
}

func (s *Servicio) asientosDeBase(ctx context.Context, cuenta *int64, periodo time.Time) ([]Movimiento, error) {
	filas, err := s.pool.Query(ctx, `
		SELECT periodo, cuenta_id, orden_id, contraparte_id, monto_centavos, tramo, creado_en
		FROM asientos
		WHERE periodo = $1 AND ($2::bigint IS NULL OR cuenta_id = $2)
		ORDER BY cuenta_id, creado_en, id`, periodo, cuenta)
	if err != nil {
		return nil, err
	}
	defer filas.Close()

	movs := []Movimiento{}
	for filas.Next() {
		var m Movimiento
		if err := filas.Scan(&m.Periodo, &m.CuentaID, &m.OrdenID, &m.ContraparteID, &m.MontoCentavos, &m.Tramo, &m.CreadoEn); err != nil {
			return nil, err
		}
		movs = append(movs, m)
	}
	return movs, filas.Err()
}
