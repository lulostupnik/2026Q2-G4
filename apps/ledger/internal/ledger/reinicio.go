package ledger

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"ledger/internal/pg"
)

const ConfirmacionReinicio = "BORRAR TODO"

type ResumenReinicio struct {
	Borradas map[string]int64 `json:"borradas"`

	Internas map[string]int64 `json:"cuentas_internas_conservadas"`
}

var tablasDeDatos = []string{

	"transiciones",
	"entregas_webhook",
	"movimientos_banco",

	"asientos",
	"saldos_apertura",
	"limites",
	"solicitudes_exportacion",

	"ordenes",

	"periodos_archivados",
	"avisos_recibidos",
	"tareas",

	"mock_egresos",
	"mock_movimientos",
}

func (s *Servicio) ReiniciarDatos(ctx context.Context) (*ResumenReinicio, error) {
	res := &ResumenReinicio{Borradas: map[string]int64{}, Internas: map[string]int64{}}

	err := pg.EnTx(ctx, s.pool, func(tx pgx.Tx) error {
		for _, tabla := range tablasDeDatos {

			tag, err := tx.Exec(ctx, fmt.Sprintf(`DELETE FROM %s`, tabla))
			if err != nil {
				return fmt.Errorf("vaciar %s: %w", tabla, err)
			}
			res.Borradas[tabla] = tag.RowsAffected()
		}

		tag, err := tx.Exec(ctx, `DELETE FROM saldos WHERE cuenta_id IN
			(SELECT id FROM cuentas WHERE tipo <> 'interna')`)
		if err != nil {
			return fmt.Errorf("vaciar saldos: %w", err)
		}
		res.Borradas["saldos"] = tag.RowsAffected()

		tag, err = tx.Exec(ctx, `DELETE FROM cuentas WHERE tipo <> 'interna'`)
		if err != nil {
			return fmt.Errorf("vaciar cuentas: %w", err)
		}
		res.Borradas["cuentas"] = tag.RowsAffected()

		if _, err := tx.Exec(ctx, `UPDATE saldos SET saldo_centavos = 0, actualizado_en = now()
			WHERE cuenta_id IN (SELECT id FROM cuentas WHERE tipo = 'interna')`); err != nil {
			return fmt.Errorf("poner en cero las internas: %w", err)
		}

		if _, err := tx.Exec(ctx, `UPDATE conciliacion
			SET ultimo_cursor = NULL, proveedor = NULL, ultima_consulta = NULL WHERE id = 1`); err != nil {
			return fmt.Errorf("reiniciar el cursor de conciliacion: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	for alias, id := range s.internas {
		res.Internas[alias] = id
	}
	slog.Warn("datos reiniciados por /admin/reinicio", "borradas", res.Borradas)
	return res, nil
}
