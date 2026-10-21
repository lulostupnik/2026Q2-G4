package ledger

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"ledger/internal/apierr"
	"ledger/internal/objetos"
	"ledger/internal/pg"
	"ledger/internal/proveedor"
	"ledger/internal/webhook"
)

const (
	EstadoPendiente   = "pendiente"
	EstadoLiquidada   = "liquidada"
	EstadoRechazada   = "rechazada"
	EstadoReservada   = "reservada"
	EstadoEnviada     = "enviada"
	EstadoAcreditada  = "acreditada"
	EstadoDevuelta    = "devuelta"
	EstadoEnDuda      = "en_duda"
	EstadoAtribuida   = "atribuida"
	EstadoNoAtribuida = "no_atribuida"
)

const (
	TipoTransferencia = "transferencia"
	TipoEgreso        = "egreso"
	TipoIngreso       = "ingreso"
)

const (
	TramoTransferencia       = "transferencia"
	TramoReserva             = "reserva"
	TramoLiquidacion         = "liquidacion"
	TramoReversa             = "reversa"
	TramoIngresoBanco        = "ingreso_banco"
	TramoIngresoAcreditacion = "ingreso_acreditacion"
	TramoSemilla             = "semilla"
)

const (
	AliasTransitoriaEgresos  = "interna:transitoria_egresos"
	AliasTransitoriaIngresos = "interna:transitoria_ingresos"
	AliasCompensacion        = "interna:compensacion_bancaria"
	AliasAjustes             = "interna:ajustes"
)

var errReintento = errors.New("orden ya registrada con esa clave de idempotencia")

type Servicio struct {
	pool *pgxpool.Pool
	prov *proveedor.Cliente
	obj  objetos.Almacen

	proveedorPorDefecto string
	notif               *webhook.Notificador
	internas            map[string]int64

	internasPorID map[int64]bool

	conciliacionIntervalo time.Duration
}

func Nuevo(ctx context.Context, pool *pgxpool.Pool, prov *proveedor.Cliente, obj objetos.Almacen, notif *webhook.Notificador, conciliacionIntervalo time.Duration, proveedorPorDefecto string) (*Servicio, error) {
	s := &Servicio{
		pool:                  pool,
		prov:                  prov,
		obj:                   obj,
		proveedorPorDefecto:   proveedorPorDefecto,
		notif:                 notif,
		internas:              map[string]int64{},
		internasPorID:         map[int64]bool{},
		conciliacionIntervalo: conciliacionIntervalo,
	}

	filas, err := pool.Query(ctx, `SELECT alias, id FROM cuentas WHERE tipo = 'interna'`)
	if err != nil {
		return nil, err
	}
	defer filas.Close()
	for filas.Next() {
		var alias string
		var id int64
		if err := filas.Scan(&alias, &id); err != nil {
			return nil, err
		}
		s.internas[alias] = id
		s.internasPorID[id] = true
	}
	if err := filas.Err(); err != nil {
		return nil, err
	}
	for _, alias := range []string{AliasTransitoriaEgresos, AliasTransitoriaIngresos, AliasCompensacion, AliasAjustes} {
		if _, ok := s.internas[alias]; !ok {
			return nil, fmt.Errorf("falta la cuenta interna %s: correr db/002_cuentas_internas.sql", alias)
		}
	}
	return s, nil
}

func (s *Servicio) interna(alias string) int64 { return s.internas[alias] }

func (s *Servicio) rechazarInterna(ids ...int64) *apierr.Error {
	for _, id := range ids {
		if s.internasPorID[id] {
			return apierr.Invalido("la cuenta %d es interna del ledger y no se opera por la API", id)
		}
	}
	return nil
}

var terminales = map[string]bool{
	EstadoLiquidada: true, EstadoRechazada: true, EstadoAcreditada: true,
	EstadoDevuelta: true, EstadoAtribuida: true, EstadoNoAtribuida: true,
}

func EsTerminal(estado string) bool { return terminales[estado] }

func (s *Servicio) avisar(o *Orden) {
	if o == nil || !EsTerminal(o.Estado) {
		return
	}
	s.notif.Notificar(webhook.Aviso{
		OrdenID:       o.ID,
		Tipo:          o.Tipo,
		Estado:        o.Estado,
		Motivo:        o.Motivo,
		OrigenID:      o.OrigenID,
		DestinoID:     o.DestinoID,
		MontoCentavos: o.MontoCentavos,
		Referencia:    o.Referencia,
	})
}

type Orden struct {
	ID            int64     `json:"id"`
	Tipo          string    `json:"tipo"`
	Estado        string    `json:"estado"`
	Referencia    string    `json:"referencia,omitempty"`
	OrigenID      *int64    `json:"origen_id,omitempty"`
	DestinoID     *int64    `json:"destino_id,omitempty"`
	CBUDestino    string    `json:"cbu_destino,omitempty"`
	MontoCentavos int64     `json:"monto_centavos"`
	Motivo        string    `json:"motivo,omitempty"`
	CreadaEn      time.Time `json:"creada_en"`
	ActualizadaEn time.Time `json:"actualizada_en"`
}

const columnasOrden = `id, tipo::text, estado::text, coalesce(referencia, ''), origen_id,
	destino_id, coalesce(cbu_destino, ''), monto_centavos, coalesce(motivo, ''),
	creada_en, actualizada_en`

func escanearOrden(fila pgx.Row) (*Orden, error) {
	var o Orden
	err := fila.Scan(&o.ID, &o.Tipo, &o.Estado, &o.Referencia, &o.OrigenID, &o.DestinoID,
		&o.CBUDestino, &o.MontoCentavos, &o.Motivo, &o.CreadaEn, &o.ActualizadaEn)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierr.NoEncontrado("no existe la orden")
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func leerOrden(ctx context.Context, c pg.Consultador, id int64) (*Orden, error) {
	return escanearOrden(c.QueryRow(ctx, `SELECT `+columnasOrden+` FROM ordenes WHERE id = $1`, id))
}

func leerOrdenPorTipo(ctx context.Context, c pg.Consultador, tipo string, id int64) (*Orden, error) {
	o, err := escanearOrden(c.QueryRow(ctx,
		`SELECT `+columnasOrden+` FROM ordenes WHERE id = $1 AND tipo = $2::tipo_orden`, id, tipo))
	if err != nil {
		return nil, err
	}
	return o, nil
}

func leerOrdenPorClave(ctx context.Context, c pg.Consultador, tipo, clave string) (*Orden, error) {
	return escanearOrden(c.QueryRow(ctx,
		`SELECT `+columnasOrden+` FROM ordenes WHERE tipo = $1::tipo_orden AND idempotency_key = $2`,
		tipo, clave))
}

func reintentoDe(ctx context.Context, c pg.Consultador, tipo, clave string, misma func(*Orden) bool) (*Orden, error) {
	o, err := leerOrdenPorClave(ctx, c, tipo, clave)
	if err != nil {
		return nil, err
	}
	if !misma(o) {
		return nil, apierr.New(http.StatusUnprocessableEntity, "clave_reutilizada",
			"la clave de idempotencia ya se uso para otra operacion; un reintento tiene que repetir los mismos datos")
	}
	return o, nil
}

func igualA(id *int64, valor int64) bool {
	return id != nil && *id == valor
}

func EstadoHTTP(o *Orden) int {
	switch o.Estado {
	case EstadoEnDuda:
		return http.StatusAccepted
	default:
		return http.StatusOK
	}
}

func resultado(o *Orden) (*Orden, error) {
	if o.Estado == EstadoRechazada {
		return o, apierr.Rechazado("rechazada", "%s", o.Motivo)
	}
	return o, nil
}
