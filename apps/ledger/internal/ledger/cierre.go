package ledger

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"ledger/internal/apierr"
	"ledger/internal/pg"
)

type SaldoCuenta struct {
	CuentaID            int64  `json:"cuenta_id"`
	Alias               string `json:"alias"`
	SaldoCentavos       int64  `json:"saldo_centavos"`
	OperacionesAbiertas int    `json:"operaciones_abiertas,omitempty"`
}

type Descuadre struct {
	CuentaID         int64 `json:"cuenta_id"`
	VigenteCentavos  int64 `json:"saldo_vigente_centavos"`
	DerivadoCentavos int64 `json:"saldo_derivado_centavos"`
}

type Cierre struct {
	Momento      time.Time     `json:"momento"`
	Transitorias []SaldoCuenta `json:"transitorias"`
	Descuadres   []Descuadre   `json:"descuadres"`

	Cierra bool `json:"cierra"`
}

func (s *Servicio) VerificarCierre(ctx context.Context) (*Cierre, error) {
	c := &Cierre{Momento: time.Now().UTC()}

	filas, err := s.pool.Query(ctx, `
		SELECT c.id, c.alias, s.saldo_centavos
		FROM cuentas c
		JOIN saldos s ON s.cuenta_id = c.id
		WHERE c.alias IN ($1, $2)
		ORDER BY c.alias`, AliasTransitoriaEgresos, AliasTransitoriaIngresos)
	if err != nil {
		return nil, err
	}
	defer filas.Close()
	for filas.Next() {
		var t SaldoCuenta
		if err := filas.Scan(&t.CuentaID, &t.Alias, &t.SaldoCentavos); err != nil {
			return nil, err
		}
		c.Transitorias = append(c.Transitorias, t)
	}
	if err := filas.Err(); err != nil {
		return nil, err
	}

	desc, err := s.pool.Query(ctx, `
		SELECT s.cuenta_id, s.saldo_centavos, coalesce(ap.saldo_centavos, 0) + coalesce(sum(a.monto_centavos), 0)
		FROM saldos s
		LEFT JOIN LATERAL (
			SELECT saldo_centavos, corte FROM saldos_apertura sa
			WHERE sa.cuenta_id = s.cuenta_id ORDER BY corte DESC LIMIT 1
		) ap ON TRUE
		LEFT JOIN asientos a
		       ON a.cuenta_id = s.cuenta_id
		      AND a.periodo >= coalesce(ap.corte, '-infinity'::date)
		GROUP BY s.cuenta_id, s.saldo_centavos, ap.saldo_centavos
		HAVING s.saldo_centavos <> coalesce(ap.saldo_centavos, 0) + coalesce(sum(a.monto_centavos), 0)`)
	if err != nil {
		return nil, err
	}
	defer desc.Close()
	for desc.Next() {
		var d Descuadre
		if err := desc.Scan(&d.CuentaID, &d.VigenteCentavos, &d.DerivadoCentavos); err != nil {
			return nil, err
		}
		c.Descuadres = append(c.Descuadres, d)
	}
	if err := desc.Err(); err != nil {
		return nil, err
	}

	c.Cierra = len(c.Descuadres) == 0
	for _, t := range c.Transitorias {
		if t.SaldoCentavos != 0 {
			c.Cierra = false
		}
	}
	return c, nil
}

type EntradaSemilla struct {
	Periodo       string `json:"periodo"`
	OrigenID      int64  `json:"origen_id"`
	DestinoID     int64  `json:"destino_id"`
	Cantidad      int    `json:"cantidad"`
	MontoCentavos int64  `json:"monto_centavos"`
}

func (s *Servicio) SembrarPeriodo(ctx context.Context, e EntradaSemilla) (int, error) {
	periodo, err := time.Parse("2006-01", e.Periodo)
	if err != nil {
		return 0, apierr.Invalido("periodo tiene que tener formato AAAA-MM")
	}
	if e.Cantidad <= 0 || e.Cantidad > 1000 {
		return 0, apierr.Invalido("cantidad tiene que estar entre 1 y 1000")
	}
	if e.MontoCentavos <= 0 {
		return 0, apierr.Invalido("monto_centavos tiene que ser positivo")
	}
	if err := pg.AsegurarParticion(ctx, s.pool, periodo); err != nil {
		return 0, err
	}

	var saldo int64
	var permiteNegativo bool
	err = s.pool.QueryRow(ctx, `
		SELECT s.saldo_centavos, c.permite_negativo
		FROM saldos s JOIN cuentas c ON c.id = s.cuenta_id
		WHERE s.cuenta_id = $1`, e.OrigenID).Scan(&saldo, &permiteNegativo)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, apierr.NoEncontrado("no existe la cuenta %d", e.OrigenID)
	}
	if err != nil {
		return 0, err
	}
	if total := e.MontoCentavos * int64(e.Cantidad); !permiteNegativo && saldo < total {
		return 0, apierr.Rechazado("fondos_insuficientes",
			"sembrar %d transferencias de %d centavos necesita %d y la cuenta %d tiene %d",
			e.Cantidad, e.MontoCentavos, total, e.OrigenID, saldo)
	}

	hechas := 0
	for i := 0; i < e.Cantidad; i++ {
		clave := fmt.Sprintf("semilla:%s:%d:%d", e.Periodo, e.OrigenID, i)
		err := pg.EnTx(ctx, s.pool, func(tx pgx.Tx) error {
			id, err := insertarOrden(ctx, tx, nuevaOrden{
				Tipo:           TipoTransferencia,
				IdempotencyKey: clave,
				OrigenID:       &e.OrigenID,
				DestinoID:      &e.DestinoID,
				MontoCentavos:  e.MontoCentavos,
				Estado:         EstadoPendiente,
			})
			if err != nil {
				return err
			}
			saldos, err := bloquearSaldos(ctx, tx, e.OrigenID, e.DestinoID)
			if err != nil {
				return err
			}
			if problema := validarFondos(saldos[e.OrigenID], e.MontoCentavos); problema != nil {
				return problema
			}
			if err := asentarPar(ctx, tx, periodo, id, e.OrigenID, e.DestinoID, e.MontoCentavos, TramoSemilla); err != nil {
				return err
			}
			_, err = transicionar(ctx, tx, id, EstadoPendiente, EstadoLiquidada, "semilla de demo")
			return err
		})
		if err != nil {
			return hechas, err
		}
		hechas++
	}
	return hechas, nil
}
