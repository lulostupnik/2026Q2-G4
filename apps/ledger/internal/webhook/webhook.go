package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	EstadoPendiente = "pendiente"
	EstadoEntregada = "entregada"
	EstadoFallida   = "fallida"
)

const (
	CabeceraFirma = "X-Ledger-Signature"
	tiempoLimite  = 5 * time.Second
	factorEspera  = 2
)

const ventanaReintento = "15 minutes"

type Aviso struct {
	OrdenID       int64  `json:"orden_id"`
	Tipo          string `json:"tipo"`
	Estado        string `json:"estado"`
	Motivo        string `json:"motivo,omitempty"`
	OrigenID      *int64 `json:"origen_id,omitempty"`
	DestinoID     *int64 `json:"destino_id,omitempty"`
	MontoCentavos int64  `json:"monto_centavos"`
	Referencia    string `json:"referencia,omitempty"`
}

type Notificador struct {
	pool     *pgxpool.Pool
	cliente  *http.Client
	url      string
	secreto  string
	intentos int
	espera   time.Duration

	pendientes sync.WaitGroup
}

func Nuevo(pool *pgxpool.Pool, url, secreto string, intentos int, espera time.Duration) *Notificador {
	if intentos <= 0 {
		intentos = 3
	}
	if espera <= 0 {
		espera = 200 * time.Millisecond
	}
	return &Notificador{
		pool:     pool,
		cliente:  &http.Client{Timeout: tiempoLimite},
		url:      url,
		secreto:  secreto,
		intentos: intentos,
		espera:   espera,
	}
}

func (n *Notificador) Habilitado() bool { return n != nil && n.url != "" }

func (n *Notificador) Notificar(a Aviso) {
	if !n.Habilitado() {
		return
	}
	n.pendientes.Add(1)
	go func() {
		defer n.pendientes.Done()
		n.entregar(context.Background(), a)
	}()
}

func (n *Notificador) Esperar(ctx context.Context) {
	if !n.Habilitado() {
		return
	}
	listo := make(chan struct{})
	go func() { n.pendientes.Wait(); close(listo) }()
	select {
	case <-listo:
	case <-ctx.Done():
	}
}

func (n *Notificador) entregar(ctx context.Context, a Aviso) {

	tag, err := n.pool.Exec(ctx, `
		INSERT INTO entregas_webhook (orden_id, estado) VALUES ($1, $2)
		ON CONFLICT (orden_id) DO UPDATE
		SET estado = EXCLUDED.estado, actualizada_en = now()
		WHERE entregas_webhook.estado <> $3
		  AND entregas_webhook.actualizada_en < now() - $4::interval`,
		a.OrdenID, EstadoPendiente, EstadoEntregada, ventanaReintento)
	if err != nil {
		slog.Error("reservar entrega de webhook", "orden", a.OrdenID, "error", err)
		return
	}
	if tag.RowsAffected() == 0 {

		return
	}

	cuerpo, err := json.Marshal(a)
	if err != nil {
		n.registrar(ctx, a.OrdenID, EstadoFallida, 0, err.Error())
		return
	}
	firma := Firmar(cuerpo, n.secreto)

	var ultimo error
	for intento := 1; intento <= n.intentos; intento++ {
		ultimo = n.enviar(ctx, cuerpo, firma)
		if ultimo == nil {
			n.registrar(ctx, a.OrdenID, EstadoEntregada, intento, "")
			return
		}
		if intento < n.intentos {
			select {
			case <-time.After(n.esperaDe(intento)):
			case <-ctx.Done():
				n.registrar(ctx, a.OrdenID, EstadoFallida, intento, ctx.Err().Error())
				return
			}
		}
	}

	slog.Warn("webhook agotado", "orden", a.OrdenID, "intentos", n.intentos, "error", ultimo)
	n.registrar(ctx, a.OrdenID, EstadoFallida, n.intentos, ultimo.Error())
}

func (n *Notificador) enviar(ctx context.Context, cuerpo []byte, firma string) error {
	pedido, err := http.NewRequestWithContext(ctx, http.MethodPost, n.url, bytes.NewReader(cuerpo))
	if err != nil {
		return err
	}
	pedido.Header.Set("Content-Type", "application/json")
	pedido.Header.Set(CabeceraFirma, firma)

	resp, err := n.cliente.Do(pedido)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("el webhook respondio %d", resp.StatusCode)
	}
	return nil
}

func (n *Notificador) esperaDe(intento int) time.Duration {
	espera := n.espera
	for i := 1; i < intento; i++ {
		espera *= factorEspera
	}
	return espera
}

func (n *Notificador) registrar(ctx context.Context, ordenID int64, estado string, intentos int, motivo string) {
	var err error
	if motivo == "" {
		_, err = n.pool.Exec(ctx, `
			INSERT INTO entregas_webhook (orden_id, estado, intentos)
			VALUES ($1, $2, $3)
			ON CONFLICT (orden_id) DO UPDATE
			SET estado = EXCLUDED.estado, intentos = EXCLUDED.intentos,
			    ultimo_error = NULL, actualizada_en = now()`, ordenID, estado, intentos)
	} else {
		_, err = n.pool.Exec(ctx, `
			INSERT INTO entregas_webhook (orden_id, estado, intentos, ultimo_error)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (orden_id) DO UPDATE
			SET estado = EXCLUDED.estado, intentos = EXCLUDED.intentos,
			    ultimo_error = EXCLUDED.ultimo_error, actualizada_en = now()`,
			ordenID, estado, intentos, motivo)
	}
	if err != nil {

		slog.Error("registrar entrega de webhook", "orden", ordenID, "error", err)
	}
}

func Firmar(cuerpo []byte, secreto string) string {
	mac := hmac.New(sha256.New, []byte(secreto))
	mac.Write(cuerpo)
	return hex.EncodeToString(mac.Sum(nil))
}
