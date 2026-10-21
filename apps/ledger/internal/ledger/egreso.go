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

type EntradaEgreso struct {
	IdempotencyKey string
	OrigenID       int64  `json:"origen_id"`
	CBUDestino     string `json:"cbu_destino"`
	MontoCentavos  int64  `json:"monto_centavos"`
}

func (s *Servicio) Egresar(ctx context.Context, e EntradaEgreso) (o *Orden, err error) {

	defer func() { s.avisar(o) }()

	if e.MontoCentavos <= 0 {
		return nil, apierr.Invalido("monto_centavos tiene que ser positivo")
	}
	if e.CBUDestino == "" {
		return nil, apierr.Invalido("falta cbu_destino")
	}
	if problema := s.rechazarInterna(e.OrigenID); problema != nil {
		return nil, problema
	}

	transitoria := s.interna(AliasTransitoriaEgresos)
	var ordenID int64
	var rechazo *apierr.Error

	err = pg.EnTx(ctx, s.pool, func(tx pgx.Tx) error {
		id, err := insertarOrden(ctx, tx, nuevaOrden{
			Tipo:           TipoEgreso,
			IdempotencyKey: e.IdempotencyKey,
			Referencia:     nuevaReferencia("EGR"),
			OrigenID:       &e.OrigenID,
			DestinoID:      &transitoria,
			CBUDestino:     e.CBUDestino,
			MontoCentavos:  e.MontoCentavos,
			Estado:         EstadoPendiente,
		})
		if err != nil {
			return err
		}
		ordenID = id

		saldos, err := bloquearSaldos(ctx, tx, e.OrigenID, transitoria)
		if err != nil {
			return err
		}
		if problema := validarFondos(saldos[e.OrigenID], e.MontoCentavos); problema != nil {
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
			e.OrigenID, transitoria, e.MontoCentavos, TramoReserva); err != nil {
			return err
		}
		_, err = transicionar(ctx, tx, id, EstadoPendiente, EstadoReservada, "")
		return err
	})

	if errors.Is(err, errReintento) {

		o, err := reintentoDe(ctx, s.pool, TipoEgreso, e.IdempotencyKey, func(o *Orden) bool {
			return igualA(o.OrigenID, e.OrigenID) && o.CBUDestino == e.CBUDestino &&
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
		o, err := leerOrden(ctx, s.pool, ordenID)
		if err != nil {
			return nil, err
		}
		return o, rechazo
	}

	return s.despachar(ctx, ordenID)
}

func (s *Servicio) despachar(ctx context.Context, ordenID int64) (*Orden, error) {
	o, err := leerOrden(ctx, s.pool, ordenID)
	if err != nil {
		return nil, err
	}

	aplicado, err := transicionar(ctx, s.pool, ordenID, EstadoReservada, EstadoEnviada, "")
	if err != nil {
		return nil, err
	}
	if !aplicado {

		return leerOrden(ctx, s.pool, ordenID)
	}

	resp, err := s.prov.OrdenarEgreso(ctx, o.Referencia, o.CBUDestino, o.MontoCentavos)
	if err != nil {

		if pe, rechazo := proveedor.EsRechazo(err); rechazo {
			motivo := fmt.Sprintf("el proveedor rechazo el pedido (%d)", pe.Codigo)
			if proveedor.EsCredencial(err) {

				motivo = fmt.Sprintf("el proveedor rechazo las credenciales (%d): revisar PROVEEDOR_API_KEY", pe.Codigo)
				slog.Error("credenciales rechazadas por el proveedor", "orden", ordenID, "codigo", pe.Codigo)
			}
			if err := s.moverEgreso(ctx, ordenID, EstadoEnviada, EstadoDevuelta, TramoReversa, motivo); err != nil {
				return nil, err
			}
			return leerOrden(ctx, s.pool, ordenID)
		}

		slog.Warn("egreso sin respuesta del proveedor", "orden", ordenID, "error", err)
		if _, err := transicionar(ctx, s.pool, ordenID, EstadoEnviada, EstadoEnDuda, "sin respuesta del proveedor"); err != nil {
			return nil, err
		}
		return leerOrden(ctx, s.pool, ordenID)
	}
	return s.aplicarRespuestaEgreso(ctx, ordenID, EstadoEnviada, resp)
}

func (s *Servicio) aplicarRespuestaEgreso(ctx context.Context, ordenID int64, desde string, resp proveedor.RespuestaEgreso) (*Orden, error) {
	switch resp.Estado {
	case proveedor.EstadoAcreditado:
		if err := s.moverEgreso(ctx, ordenID, desde, EstadoAcreditada, TramoLiquidacion, ""); err != nil {
			return nil, err
		}
	case proveedor.EstadoRechazado:

		if err := s.moverEgreso(ctx, ordenID, desde, EstadoDevuelta, TramoReversa, resp.Motivo); err != nil {
			return nil, err
		}
	default:
		if _, err := transicionar(ctx, s.pool, ordenID, desde, EstadoEnDuda, "el proveedor la informa pendiente"); err != nil {
			return nil, err
		}
	}
	return leerOrden(ctx, s.pool, ordenID)
}

func (s *Servicio) moverEgreso(ctx context.Context, ordenID int64, desde, hasta, tramo, motivo string) error {
	transitoria := s.interna(AliasTransitoriaEgresos)
	compensacion := s.interna(AliasCompensacion)

	return pg.EnTx(ctx, s.pool, func(tx pgx.Tx) error {
		aplicado, err := transicionar(ctx, tx, ordenID, desde, hasta, motivo)
		if err != nil || !aplicado {
			return err
		}
		o, err := leerOrden(ctx, tx, ordenID)
		if err != nil {
			return err
		}

		destino := compensacion
		if hasta == EstadoDevuelta {
			destino = *o.OrigenID
		}
		if _, err := bloquearSaldos(ctx, tx, transitoria, destino); err != nil {
			return err
		}
		return asentarPar(ctx, tx, pg.InicioDeMes(time.Now()), ordenID,
			transitoria, destino, o.MontoCentavos, tramo)
	})
}

func (s *Servicio) ConsultarEgreso(ctx context.Context, id int64) (o *Orden, err error) {

	defer func() { s.avisar(o) }()

	o, err = leerOrdenPorTipo(ctx, s.pool, TipoEgreso, id)
	if err != nil {
		return nil, err
	}
	if o.Estado != EstadoEnDuda {
		return o, nil
	}

	resp, err := s.prov.ConsultarEgreso(ctx, o.Referencia)
	if err != nil {

		if proveedor.EsCredencial(err) {
			slog.Error("no se puede resolver la duda: credenciales rechazadas",
				"orden", id, "error", err)
		} else {
			slog.Warn("no se pudo resolver la duda", "orden", id, "error", err)
		}
		return o, nil
	}
	return s.aplicarRespuestaEgreso(ctx, id, EstadoEnDuda, resp)
}
