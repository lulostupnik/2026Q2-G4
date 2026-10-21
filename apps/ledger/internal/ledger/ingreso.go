package ledger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"ledger/internal/apierr"
	"ledger/internal/pg"
	"ledger/internal/proveedor"
)

type EntradaIngreso struct {
	IdempotencyKey string
	DestinoID      int64 `json:"destino_id"`
	MontoCentavos  int64 `json:"monto_centavos"`
}

func (s *Servicio) CrearIntencionIngreso(ctx context.Context, e EntradaIngreso) (*Orden, error) {
	if e.MontoCentavos <= 0 {
		return nil, apierr.Invalido("monto_centavos tiene que ser positivo")
	}
	if problema := s.rechazarInterna(e.DestinoID); problema != nil {
		return nil, problema
	}

	var ordenID int64
	err := pg.EnTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := bloquearSaldos(ctx, tx, e.DestinoID); err != nil {
			return err
		}
		id, err := insertarOrden(ctx, tx, nuevaOrden{
			Tipo:           TipoIngreso,
			IdempotencyKey: e.IdempotencyKey,
			Referencia:     nuevaReferencia("ING"),
			DestinoID:      &e.DestinoID,
			MontoCentavos:  e.MontoCentavos,
			Estado:         EstadoPendiente,
		})
		ordenID = id
		return err
	})
	if errors.Is(err, errReintento) {
		return reintentoDe(ctx, s.pool, TipoIngreso, e.IdempotencyKey, func(o *Orden) bool {
			return igualA(o.DestinoID, e.DestinoID) && o.MontoCentavos == e.MontoCentavos
		})
	}
	if err != nil {
		return nil, err
	}
	return leerOrden(ctx, s.pool, ordenID)
}

func (s *Servicio) ConsultarIngreso(ctx context.Context, id int64) (o *Orden, err error) {

	defer func() { s.avisar(o) }()

	o, err = leerOrdenPorTipo(ctx, s.pool, TipoIngreso, id)
	if err != nil {
		return nil, err
	}
	if o.Estado == EstadoPendiente {
		if _, err := s.Conciliar(ctx); err != nil {

			slog.Warn("conciliacion fallida", "error", err)
		}
	}
	return leerOrdenPorTipo(ctx, s.pool, TipoIngreso, id)
}

func (s *Servicio) SimularDeposito(ctx context.Context, ordenID int64) (*Orden, error) {
	o, err := leerOrdenPorTipo(ctx, s.pool, TipoIngreso, ordenID)
	if err != nil {
		return nil, err
	}
	if o.Estado != EstadoPendiente {
		return nil, apierr.New(409, "ingreso_no_pendiente",
			"la carga %d ya esta %s: no hay nada que depositar", o.ID, o.Estado)
	}
	if o.Referencia == "" {
		return nil, apierr.New(409, "ingreso_sin_referencia",
			"la carga %d no tiene referencia con la que atribuir el deposito", o.ID)
	}

	if _, err := s.prov.RegistrarDeposito(ctx, "banco-"+o.Referencia, o.Referencia, o.MontoCentavos); err != nil {
		return nil, apierr.New(502, "proveedor_no_acepto_el_deposito",
			"el proveedor no registro el deposito: %v", err)
	}

	if _, err := s.conciliarAhora(ctx); err != nil {
		slog.Warn("conciliacion posterior al deposito simulado", "orden", o.ID, "error", err)
	}
	return s.ConsultarIngreso(ctx, ordenID)
}

func (s *Servicio) conciliarAhora(ctx context.Context) (ResumenConciliacion, error) {
	if _, err := s.pool.Exec(ctx,
		`UPDATE conciliacion SET ultima_consulta = NULL WHERE id = 1`); err != nil {
		return ResumenConciliacion{}, err
	}
	return s.Conciliar(ctx)
}

type ResumenConciliacion struct {
	Consultado bool `json:"consultado"`

	Proveedor      string `json:"proveedor,omitempty"`
	ProveedorNuevo bool   `json:"proveedor_nuevo,omitempty"`
	Movimientos    int    `json:"movimientos"`
	Atribuidos     int    `json:"atribuidos"`
	NoAtribuidos   int    `json:"no_atribuidos"`
	Evidencia      string `json:"evidencia,omitempty"`
}

func (s *Servicio) Conciliar(ctx context.Context) (ResumenConciliacion, error) {
	var res ResumenConciliacion

	var cursor, proveedorDelCursor string
	err := s.pool.QueryRow(ctx, `
		UPDATE conciliacion SET ultima_consulta = now()
		WHERE id = 1
		  AND (ultima_consulta IS NULL OR ultima_consulta < now() - $1::interval)
		RETURNING coalesce(ultimo_cursor, ''), coalesce(proveedor, '')`,
		fmt.Sprintf("%d seconds", int(s.conciliacionIntervalo.Seconds()))).
		Scan(&cursor, &proveedorDelCursor)
	if errors.Is(err, pgx.ErrNoRows) {
		return res, nil
	}
	if err != nil {
		return res, err
	}
	res.Consultado = true

	activo, err := s.proveedorVigente(ctx)
	if err != nil {
		return res, err
	}
	if proveedorDelCursor != activo {
		slog.Info("cambio el proveedor: se descarta el cursor de conciliacion",
			"antes", proveedorDelCursor, "ahora", activo, "cursor_descartado", cursor)
		cursor = ""
		res.ProveedorNuevo = true
		if _, err := s.pool.Exec(ctx,
			`UPDATE conciliacion SET ultimo_cursor = NULL, proveedor = $1 WHERE id = 1`,
			activo); err != nil {
			return res, err
		}
	}
	res.Proveedor = activo

	movs, siguiente, crudo, err := s.prov.Movimientos(ctx, cursor)
	if err != nil {
		return res, err
	}

	clave := fmt.Sprintf("conciliacion/%s/%s.json",
		time.Now().UTC().Format("2006/01/02"), nuevaReferencia("CON"))
	if uri, err := s.obj.Guardar(ctx, clave, crudo, "application/json"); err != nil {
		return res, fmt.Errorf("guardar evidencia de conciliacion: %w", err)
	} else {
		res.Evidencia = uri
	}

	for _, m := range movs {
		atribuido, err := s.atribuir(ctx, m)
		if err != nil {
			return res, err
		}
		res.Movimientos++
		if atribuido {
			res.Atribuidos++
		} else {
			res.NoAtribuidos++
		}
	}

	if siguiente != "" {

		if _, err := s.pool.Exec(ctx,
			`UPDATE conciliacion SET ultimo_cursor = $1, proveedor = $2 WHERE id = 1`,
			siguiente, activo); err != nil {
			return res, err
		}
	}
	return res, nil
}

func (s *Servicio) proveedorVigente(ctx context.Context) (string, error) {
	p, err := s.VerProveedor(ctx)
	if err != nil {
		return "", err
	}
	return p.URL, nil
}

func (s *Servicio) atribuir(ctx context.Context, m proveedor.Movimiento) (bool, error) {
	compensacion := s.interna(AliasCompensacion)
	transitoria := s.interna(AliasTransitoriaIngresos)
	periodo := pg.InicioDeMes(time.Now())

	var atribuido bool
	err := pg.EnTx(ctx, s.pool, func(tx pgx.Tx) error {

		var movID int64
		err := tx.QueryRow(ctx, `
			INSERT INTO movimientos_banco (id_origen, referencia, monto_centavos, ocurrido_en)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (id_origen) DO NOTHING
			RETURNING id`,
			m.IDOrigen, nuloSiVacio(m.Referencia), m.MontoCentavos, m.OcurridoEn).Scan(&movID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}

		orden, err := buscarIntencion(ctx, tx, m.Referencia)
		if err != nil {
			return err
		}

		if orden != nil && orden.MontoCentavos == m.MontoCentavos && orden.Estado == EstadoPendiente {
			destino := *orden.DestinoID
			if _, err := bloquearSaldos(ctx, tx, compensacion, transitoria, destino); err != nil {
				return err
			}

			if err := asentarPar(ctx, tx, periodo, orden.ID, compensacion, transitoria, m.MontoCentavos, TramoIngresoBanco); err != nil {
				return err
			}
			if err := asentarPar(ctx, tx, periodo, orden.ID, transitoria, destino, m.MontoCentavos, TramoIngresoAcreditacion); err != nil {
				return err
			}
			if _, err := transicionar(ctx, tx, orden.ID, EstadoPendiente, EstadoAtribuida, ""); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx,
				`UPDATE movimientos_banco SET orden_id = $1, atribuido = TRUE WHERE id = $2`,
				orden.ID, movID); err != nil {
				return err
			}
			atribuido = true
			return nil
		}

		motivo := "referencia desconocida"
		if orden != nil {
			motivo = fmt.Sprintf("el monto informado (%d) no coincide con la carga %d",
				m.MontoCentavos, orden.MontoCentavos)
			if _, err := transicionar(ctx, tx, orden.ID, EstadoPendiente, EstadoNoAtribuida, motivo); err != nil {
				return err
			}
		}

		huerfana, err := insertarOrden(ctx, tx, nuevaOrden{
			Tipo:           TipoIngreso,
			IdempotencyKey: "mov:" + m.IDOrigen,
			DestinoID:      &transitoria,
			MontoCentavos:  m.MontoCentavos,
			Estado:         EstadoPendiente,
		})
		if err != nil {
			return err
		}
		if _, err := bloquearSaldos(ctx, tx, compensacion, transitoria); err != nil {
			return err
		}
		if err := asentarPar(ctx, tx, periodo, huerfana, compensacion, transitoria, m.MontoCentavos, TramoIngresoBanco); err != nil {
			return err
		}
		if _, err := transicionar(ctx, tx, huerfana, EstadoPendiente, EstadoNoAtribuida, motivo); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE movimientos_banco SET orden_id = $1 WHERE id = $2`, huerfana, movID)
		return err
	})
	if errors.Is(err, errReintento) {

		return false, nil
	}
	return atribuido, err
}

func buscarIntencion(ctx context.Context, c pg.Consultador, referencia string) (*Orden, error) {
	if referencia == "" {
		return nil, nil
	}
	o, err := escanearOrden(c.QueryRow(ctx,
		`SELECT `+columnasOrden+` FROM ordenes WHERE referencia = $1 AND tipo = 'ingreso'`, referencia))
	var e *apierr.Error
	if errors.As(err, &e) && e.Codigo == "no_encontrado" {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return o, nil
}
