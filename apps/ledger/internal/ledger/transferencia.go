package ledger

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"ledger/internal/apierr"
	"ledger/internal/pg"
)

type EntradaTransferencia struct {
	IdempotencyKey string
	OrigenID       int64 `json:"origen_id"`
	DestinoID      int64 `json:"destino_id"`
	MontoCentavos  int64 `json:"monto_centavos"`
}

func (s *Servicio) Transferir(ctx context.Context, e EntradaTransferencia) (o *Orden, err error) {

	defer func() { s.avisar(o) }()

	if e.MontoCentavos <= 0 {
		return nil, apierr.Invalido("monto_centavos tiene que ser positivo")
	}
	if e.OrigenID == e.DestinoID {
		return nil, apierr.Invalido("origen y destino no pueden ser la misma cuenta")
	}
	if problema := s.rechazarInterna(e.OrigenID, e.DestinoID); problema != nil {
		return nil, problema
	}

	var orden *Orden
	var rechazo *apierr.Error

	err = pg.EnTx(ctx, s.pool, func(tx pgx.Tx) error {
		id, err := insertarOrden(ctx, tx, nuevaOrden{
			Tipo:           TipoTransferencia,
			IdempotencyKey: e.IdempotencyKey,
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
			rechazo = problema
			return rechazar(ctx, tx, id, problema)
		}
		if !saldos[e.DestinoID].Activa {
			problema := apierr.Rechazado("cuenta_inactiva", "la cuenta de destino no esta activa")
			rechazo = problema
			return rechazar(ctx, tx, id, problema)
		}
		problema, err := validarLimite(ctx, tx, e.OrigenID, e.MontoCentavos)
		if err != nil {
			return err
		}
		if problema != nil {
			rechazo = problema
			return rechazar(ctx, tx, id, problema)
		}

		if err := asentarPar(ctx, tx, pg.InicioDeMes(time.Now()), id,
			e.OrigenID, e.DestinoID, e.MontoCentavos, TramoTransferencia); err != nil {
			return err
		}
		if _, err := transicionar(ctx, tx, id, EstadoPendiente, EstadoLiquidada, ""); err != nil {
			return err
		}
		orden, err = leerOrden(ctx, tx, id)
		return err
	})

	if errors.Is(err, errReintento) {
		o, err := reintentoDe(ctx, s.pool, TipoTransferencia, e.IdempotencyKey, func(o *Orden) bool {
			return igualA(o.OrigenID, e.OrigenID) && igualA(o.DestinoID, e.DestinoID) &&
				o.MontoCentavos == e.MontoCentavos
		})
		if err != nil {
			return nil, err
		}
		return resultado(o)
	}
	if err != nil {
		return nil, err
	}
	if rechazo != nil {
		o, err := leerOrdenPorClave(ctx, s.pool, TipoTransferencia, e.IdempotencyKey)
		if err != nil {
			return nil, err
		}
		return o, rechazo
	}
	return orden, nil
}

func rechazar(ctx context.Context, tx pgx.Tx, ordenID int64, problema *apierr.Error) error {
	_, err := transicionar(ctx, tx, ordenID, EstadoPendiente, EstadoRechazada, problema.Mensaje)
	return err
}

func (s *Servicio) Consultar(ctx context.Context, tipo string, id int64) (*Orden, error) {
	o, err := leerOrdenPorTipo(ctx, s.pool, tipo, id)
	if err != nil {
		return nil, err
	}
	return o, nil
}
