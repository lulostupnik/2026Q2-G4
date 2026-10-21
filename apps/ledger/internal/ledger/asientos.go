package ledger

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"ledger/internal/apierr"
	"ledger/internal/pg"
)

type saldoBloqueado struct {
	CuentaID        int64
	Saldo           int64
	Activa          bool
	PermiteNegativo bool
}

func bloquearSaldos(ctx context.Context, tx pgx.Tx, ids ...int64) (map[int64]*saldoBloqueado, error) {
	unicos := make([]int64, 0, len(ids))
	visto := map[int64]bool{}
	for _, id := range ids {
		if !visto[id] {
			visto[id] = true
			unicos = append(unicos, id)
		}
	}
	sort.Slice(unicos, func(i, j int) bool { return unicos[i] < unicos[j] })

	res := make(map[int64]*saldoBloqueado, len(unicos))
	for _, id := range unicos {
		var s saldoBloqueado
		s.CuentaID = id
		err := tx.QueryRow(ctx, `
			SELECT s.saldo_centavos, c.activa, c.permite_negativo
			FROM saldos s
			JOIN cuentas c ON c.id = s.cuenta_id
			WHERE s.cuenta_id = $1
			FOR UPDATE OF s`, id).Scan(&s.Saldo, &s.Activa, &s.PermiteNegativo)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NoEncontrado("no existe la cuenta %d", id)
		}
		if err != nil {
			return nil, err
		}
		res[id] = &s
	}
	return res, nil
}

func validarFondos(s *saldoBloqueado, monto int64) *apierr.Error {
	if !s.Activa {
		return apierr.Rechazado("cuenta_inactiva", "la cuenta %d no esta activa", s.CuentaID)
	}
	if !s.PermiteNegativo && s.Saldo < monto {
		return apierr.Rechazado("fondos_insuficientes",
			"saldo %d insuficiente para %d centavos", s.Saldo, monto)
	}
	return nil
}

func validarLimite(ctx context.Context, c pg.Consultador, cuentaID, monto int64) (*apierr.Error, error) {
	var max int64
	err := c.QueryRow(ctx,
		`SELECT max_por_operacion_centavos FROM limites WHERE cuenta_id = $1`, cuentaID).Scan(&max)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if monto > max {
		return apierr.Rechazado("limite_superado",
			"el monto %d supera el limite por operacion de %d centavos", monto, max), nil
	}
	return nil, nil
}

func asentarPar(ctx context.Context, tx pgx.Tx, periodo time.Time, ordenID, origenID, destinoID, monto int64, tramo string) error {
	if monto <= 0 {
		return fmt.Errorf("monto invalido en asiento: %d", monto)
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO asientos (periodo, orden_id, cuenta_id, contraparte_id, monto_centavos, tramo)
		VALUES ($1, $2, $3, $4, $5, $6), ($1, $2, $4, $3, $7, $6)`,
		periodo, ordenID, origenID, destinoID, -monto, tramo, monto)
	if err != nil {
		return err
	}

	_, err = tx.Exec(ctx, `
		UPDATE saldos
		SET saldo_centavos = saldo_centavos + CASE cuenta_id WHEN $1 THEN $3::bigint ELSE $4::bigint END,
		    actualizado_en = now()
		WHERE cuenta_id IN ($1, $2)`, origenID, destinoID, -monto, monto)
	return err
}

func insertarOrden(ctx context.Context, tx pgx.Tx, o nuevaOrden) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, `
		INSERT INTO ordenes (tipo, idempotency_key, referencia, origen_id, destino_id,
		                     cbu_destino, monto_centavos, estado)
		VALUES ($1::tipo_orden, $2, $3, $4, $5, $6, $7, $8::estado_orden)
		RETURNING id`,
		o.Tipo, o.IdempotencyKey, nuloSiVacio(o.Referencia), o.OrigenID, o.DestinoID,
		nuloSiVacio(o.CBUDestino), o.MontoCentavos, o.Estado).Scan(&id)
	if pg.EsViolacionUnica(err, "ordenes_tipo_idempotency_key_key") {
		return 0, errReintento
	}
	if err != nil {
		return 0, err
	}
	if err := registrarTransicion(ctx, tx, id, "", o.Estado, ""); err != nil {
		return 0, err
	}
	return id, nil
}

type nuevaOrden struct {
	Tipo           string
	IdempotencyKey string
	Referencia     string
	OrigenID       *int64
	DestinoID      *int64
	CBUDestino     string
	MontoCentavos  int64
	Estado         string
}

func nuloSiVacio(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func transicionar(ctx context.Context, c pg.Consultador, ordenID int64, desde, hasta, motivo string) (bool, error) {
	tag, err := c.Exec(ctx, `
		UPDATE ordenes
		SET estado = $3::estado_orden, motivo = $4, actualizada_en = now()
		WHERE id = $1 AND estado = $2::estado_orden`,
		ordenID, desde, hasta, nuloSiVacio(motivo))
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	return true, registrarTransicion(ctx, c, ordenID, desde, hasta, motivo)
}

func registrarTransicion(ctx context.Context, c pg.Consultador, ordenID int64, desde, hasta, motivo string) error {
	var desdeVal any
	if desde != "" {
		desdeVal = desde
	}
	_, err := c.Exec(ctx, `
		INSERT INTO transiciones (orden_id, desde, hasta, motivo)
		VALUES ($1, $2::estado_orden, $3::estado_orden, $4)`,
		ordenID, desdeVal, hasta, nuloSiVacio(motivo))
	return err
}

func nuevaReferencia(prefijo string) string {
	var b [5]byte
	if _, err := rand.Read(b[:]); err != nil {

		panic(fmt.Sprintf("generar referencia: %v", err))
	}
	codificador := base32.StdEncoding.WithPadding(base32.NoPadding)
	return prefijo + "-" + codificador.EncodeToString(b[:])
}
