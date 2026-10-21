package ledger

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"ledger/internal/apierr"
	"ledger/internal/pg"
)

type EntradaCuenta struct {
	Alias string `json:"alias"`
	Tipo  string `json:"tipo"`

	SaldoInicialCentavos int64 `json:"saldo_inicial_centavos,omitempty"`

	Titular string `json:"-"`
}

type Cuenta struct {
	ID       int64     `json:"id"`
	Alias    string    `json:"alias"`
	Tipo     string    `json:"tipo"`
	Activa   bool      `json:"activa"`
	CreadaEn time.Time `json:"creada_en"`
}

func (s *Servicio) CrearCuenta(ctx context.Context, e EntradaCuenta) (*Cuenta, error) {
	if e.Alias == "" {
		return nil, apierr.Invalido("falta alias")
	}
	if e.Tipo != "usuario" && e.Tipo != "comercio" {
		return nil, apierr.Invalido("tipo tiene que ser usuario o comercio")
	}
	if e.SaldoInicialCentavos < 0 {
		return nil, apierr.Invalido("saldo_inicial_centavos no puede ser negativo")
	}

	var c Cuenta
	err := pg.EnTx(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO cuentas (alias, tipo, titular) VALUES ($1, $2::tipo_cuenta, NULLIF($3, ''))
			RETURNING id, alias, tipo::text, activa, creada_en`,
			e.Alias, e.Tipo, e.Titular).Scan(&c.ID, &c.Alias, &c.Tipo, &c.Activa, &c.CreadaEn)
		if pg.EsViolacionUnica(err, "cuentas_alias_key") {
			return apierr.New(409, "alias_en_uso", "ya existe una cuenta con el alias %s", e.Alias)
		}
		if pg.EsViolacionUnica(err, "cuentas_titular_key") {
			return apierr.New(409, "ya_tiene_cuenta", "el usuario ya tiene una cuenta")
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO saldos (cuenta_id) VALUES ($1)`, c.ID); err != nil {
			return err
		}
		if e.SaldoInicialCentavos == 0 {
			return nil
		}

		ajustes := s.interna(AliasAjustes)
		orden, err := insertarOrden(ctx, tx, nuevaOrden{
			Tipo:           TipoTransferencia,
			IdempotencyKey: "apertura:" + e.Alias,
			OrigenID:       &ajustes,
			DestinoID:      &c.ID,
			MontoCentavos:  e.SaldoInicialCentavos,
			Estado:         EstadoPendiente,
		})
		if err != nil {
			return err
		}
		if _, err := bloquearSaldos(ctx, tx, ajustes, c.ID); err != nil {
			return err
		}
		if err := asentarPar(ctx, tx, pg.InicioDeMes(time.Now()), orden,
			ajustes, c.ID, e.SaldoInicialCentavos, TramoTransferencia); err != nil {
			return err
		}
		_, err = transicionar(ctx, tx, orden, EstadoPendiente, EstadoLiquidada, "apertura de cuenta")
		return err
	})
	if err != nil {
		return nil, err
	}
	return &c, nil
}

type Saldo struct {
	CuentaID int64 `json:"cuenta_id"`

	VigenteCentavos int64 `json:"saldo_vigente_centavos"`

	DerivadoCentavos int64 `json:"saldo_derivado_centavos"`
	Coincide         bool  `json:"coincide"`
}

func (s *Servicio) Saldo(ctx context.Context, cuentaID int64) (*Saldo, error) {
	sa := Saldo{CuentaID: cuentaID}
	err := s.pool.QueryRow(ctx, `
		WITH apertura AS (
			SELECT saldo_centavos, corte
			FROM saldos_apertura
			WHERE cuenta_id = $1
			ORDER BY corte DESC
			LIMIT 1
		)
		SELECT s.saldo_centavos,
		       coalesce((SELECT saldo_centavos FROM apertura), 0)
		       + coalesce((SELECT sum(a.monto_centavos) FROM asientos a
		                   WHERE a.cuenta_id = $1
		                     AND a.periodo >= coalesce((SELECT corte FROM apertura), '-infinity'::date)), 0)
		FROM saldos s
		WHERE s.cuenta_id = $1`, cuentaID).Scan(&sa.VigenteCentavos, &sa.DerivadoCentavos)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierr.NoEncontrado("no existe la cuenta %d", cuentaID)
	}
	if err != nil {
		return nil, err
	}
	sa.Coincide = sa.VigenteCentavos == sa.DerivadoCentavos
	return &sa, nil
}

type Movimiento struct {
	Periodo time.Time `json:"periodo"`

	CuentaID      int64     `json:"cuenta_id"`
	OrdenID       int64     `json:"orden_id"`
	ContraparteID int64     `json:"contraparte_id"`
	MontoCentavos int64     `json:"monto_centavos"`
	Tramo         string    `json:"tramo"`
	CreadoEn      time.Time `json:"creado_en"`
}

func (s *Servicio) Movimientos(ctx context.Context, cuentaID int64, limite, desplazamiento int) ([]Movimiento, error) {
	if limite <= 0 || limite > 200 {
		limite = 50
	}
	if desplazamiento < 0 {
		desplazamiento = 0
	}
	filas, err := s.pool.Query(ctx, `
		SELECT periodo, cuenta_id, orden_id, contraparte_id, monto_centavos, tramo, creado_en
		FROM asientos
		WHERE cuenta_id = $1
		ORDER BY creado_en DESC, id DESC
		LIMIT $2 OFFSET $3`, cuentaID, limite, desplazamiento)
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
