package ledger

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"ledger/internal/apierr"
	"ledger/internal/pg"
)

type CuentaDetalle struct {
	ID                   int64     `json:"id"`
	Alias                string    `json:"alias"`
	Tipo                 string    `json:"tipo"`
	Activa               bool      `json:"activa"`
	PermiteNegativo      bool      `json:"permite_negativo"`
	SaldoCentavos        int64     `json:"saldo_centavos"`
	LimitePorOperacion   *int64    `json:"limite_por_operacion_centavos,omitempty"`
	OperacionesUltimoMes int64     `json:"operaciones_ultimo_mes"`
	CreadaEn             time.Time `json:"creada_en"`
}

const seleccionCuenta = `
	SELECT c.id, c.alias, c.tipo::text, c.activa, c.permite_negativo,
	       coalesce(s.saldo_centavos, 0),
	       l.max_por_operacion_centavos,
	       (SELECT count(*) FROM asientos a
	         WHERE a.cuenta_id = c.id
	           AND a.periodo >= date_trunc('month', now() - interval '30 days')::date
	           AND a.creado_en >= now() - interval '30 days'),
	       c.creada_en
	FROM cuentas c
	LEFT JOIN saldos s  ON s.cuenta_id = c.id
	LEFT JOIN limites l ON l.cuenta_id = c.id`

func escanearCuenta(fila pgx.Row) (*CuentaDetalle, error) {
	var c CuentaDetalle
	err := fila.Scan(&c.ID, &c.Alias, &c.Tipo, &c.Activa, &c.PermiteNegativo,
		&c.SaldoCentavos, &c.LimitePorOperacion, &c.OperacionesUltimoMes, &c.CreadaEn)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

type CuentaPublica struct {
	ID     int64  `json:"id"`
	Alias  string `json:"alias"`
	Tipo   string `json:"tipo"`
	Activa bool   `json:"activa"`
}

func (c *CuentaDetalle) Publica() CuentaPublica {
	return CuentaPublica{ID: c.ID, Alias: c.Alias, Tipo: c.Tipo, Activa: c.Activa}
}

func (s *Servicio) VerCuenta(ctx context.Context, id int64) (*CuentaDetalle, error) {
	c, err := escanearCuenta(s.pool.QueryRow(ctx, seleccionCuenta+` WHERE c.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierr.NoEncontrado("no existe la cuenta %d", id)
	}
	return c, err
}

func (s *Servicio) ListarCuentas(ctx context.Context, tipo string, limite, desplazamiento int) ([]CuentaDetalle, error) {
	if limite <= 0 || limite > 200 {
		limite = 50
	}
	if desplazamiento < 0 {
		desplazamiento = 0
	}
	if tipo != "" && tipo != "usuario" && tipo != "comercio" && tipo != "interna" {
		return nil, apierr.Invalido("tipo tiene que ser usuario, comercio o interna")
	}

	filas, err := s.pool.Query(ctx, seleccionCuenta+`
		WHERE ($1 = '' OR c.tipo::text = $1)
		ORDER BY c.id
		LIMIT $2 OFFSET $3`, tipo, limite, desplazamiento)
	if err != nil {
		return nil, err
	}
	defer filas.Close()

	cuentas := []CuentaDetalle{}
	for filas.Next() {
		c, err := escanearCuenta(filas)
		if err != nil {
			return nil, err
		}
		cuentas = append(cuentas, *c)
	}
	return cuentas, filas.Err()
}

type CambioCuenta struct {
	Activa             *bool  `json:"activa,omitempty"`
	LimitePorOperacion *int64 `json:"limite_por_operacion_centavos,omitempty"`
}

func (s *Servicio) ActualizarCuenta(ctx context.Context, id int64, c CambioCuenta) (*CuentaDetalle, error) {
	if c.Activa == nil && c.LimitePorOperacion == nil {
		return nil, apierr.Invalido("no hay nada que cambiar: mandá activa o limite_por_operacion_centavos")
	}
	if c.LimitePorOperacion != nil && *c.LimitePorOperacion < 0 {
		return nil, apierr.Invalido("el limite no puede ser negativo")
	}

	err := pg.EnTx(ctx, s.pool, func(tx pgx.Tx) error {
		var existe bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM cuentas WHERE id = $1)`, id).Scan(&existe); err != nil {
			return err
		}
		if !existe {
			return apierr.NoEncontrado("no existe la cuenta %d", id)
		}

		if c.Activa != nil {
			if _, err := tx.Exec(ctx, `UPDATE cuentas SET activa = $2 WHERE id = $1`, id, *c.Activa); err != nil {
				return err
			}
		}
		if c.LimitePorOperacion != nil {
			if _, err := tx.Exec(ctx, `
				INSERT INTO limites (cuenta_id, max_por_operacion_centavos) VALUES ($1, $2)
				ON CONFLICT (cuenta_id) DO UPDATE SET max_por_operacion_centavos = EXCLUDED.max_por_operacion_centavos`,
				id, *c.LimitePorOperacion); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.VerCuenta(ctx, id)
}

func (s *Servicio) CuentaDeTitular(ctx context.Context, sub string) (*CuentaDetalle, error) {
	c, err := escanearCuenta(s.pool.QueryRow(ctx, seleccionCuenta+` WHERE c.titular = $1`, sub))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return c, err
}
