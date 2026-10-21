package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ledger/db"
	"ledger/internal/config"
	"ledger/internal/cors"
	"ledger/internal/ledger"
	"ledger/internal/mock"
	"ledger/internal/objetos"
	"ledger/internal/pg"
	"ledger/internal/proveedor"
	"ledger/internal/sesion"
	"ledger/internal/webhook"
)

var (
	version   = "dev"
	arranque  = time.Now()
	instancia = nombreDeInstancia()
)

func nombreDeInstancia() string {
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return "desconocida"
}

func main() {

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	if err := correr(); err != nil {
		slog.Error("la api termino con error", "error", err)
		os.Exit(1)
	}
}

func correr() error {
	cfg, err := config.Cargar()
	if err != nil {
		return err
	}
	if err := cfg.ValidarAPI(); err != nil {
		return err
	}

	ctx, cancelar := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancelar()

	arranque, cancelarArranque := context.WithTimeout(ctx, 30*time.Second)
	defer cancelarArranque()

	pool, err := pg.Abrir(arranque, cfg.DBURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	aplicadas, err := db.Aplicar(arranque, pool)
	if err != nil {
		return err
	}
	if len(aplicadas) > 0 {
		slog.Info("migraciones aplicadas", "versiones", aplicadas)
	}

	if err := pg.AsegurarParticiones(arranque, pool, -1, 1); err != nil {
		return err
	}

	alm, err := objetos.Desde(arranque, cfg.Region, cfg.Bucket, cfg.ObjetosDir)
	if err != nil {
		return err
	}

	notif := webhook.Nuevo(pool, cfg.WebhookURL, cfg.WebhookSecreto, cfg.WebhookIntentos, cfg.WebhookEspera)
	if notif.Habilitado() {
		slog.Info("webhook habilitado", "url", cfg.WebhookURL, "intentos", cfg.WebhookIntentos)
	}

	receptor := webhook.NuevoReceptor(pool, cfg.WebhookSecreto)

	prov := proveedor.NuevoConClave(cfg.ProveedorURL, cfg.ProveedorTimeout, cfg.ProveedorClave)
	svc, err := ledger.Nuevo(arranque, pool, prov, alm, notif, cfg.ConciliacionIntervalo, cfg.ProveedorURL)
	if err != nil {
		return err
	}

	simulado := mock.Nuevo(pool, mock.Opciones{
		Clave: cfg.MockClave, DemoraSinRespuesta: cfg.MockDemoraSinRespuesta})
	if cfg.MockClave == "" {
		slog.Warn("MOCK_ABIERTO=1: /servicio-mock corre sin clave, solo para desarrollo local")
	}

	politica := cors.Nueva(cfg.CorsOrigenes)
	if politica.Activa() {
		slog.Info("CORS habilitado", "origenes", politica.Origenes())
	}

	sesiones := sesion.Nuevo(cfg.CognitoIssuer, cfg.CognitoClientID)
	if sesiones.Habilitado() {
		slog.Info("sesiones con Cognito", "issuer", sesiones.Issuer())
	} else {
		slog.Warn("AUTH_ABIERTA=1: las rutas de negocio no exigen sesion, solo para desarrollo local")
	}

	prov.ConResolver(func(ctx context.Context) string {
		p, err := svc.VerProveedor(ctx)
		if err != nil {
			slog.Error("no se pudo leer el proveedor activo, se usa PROVEEDOR_URL", "error", err)
			return ""
		}
		return p.URL
	})

	api := &API{
		svc: svc, pool: pool, mock: simulado, cors: politica,
		sesiones: sesiones, avisos: receptor, adminClave: cfg.AdminClave,
		bienvenida: cfg.Bienvenida,
		secretos: Secretos{
			Mock: cfg.MockClave, Proveedor: cfg.ProveedorClave,
			Webhook: cfg.WebhookSecreto, Admin: cfg.AdminClave,
		},
	}

	slog.Info("huellas de los secretos",
		"mock", huella("mock", cfg.MockClave),
		"proveedor", huella("mock", cfg.ProveedorClave),
		"webhook", huella("webhook", cfg.WebhookSecreto))
	srv := &http.Server{
		Addr:              ":" + cfg.Puerto,
		Handler:           registrar(api),
		ReadHeaderTimeout: 5 * time.Second,

		WriteTimeout: cfg.ProveedorTimeout + 10*time.Second,
		IdleTimeout:  60 * time.Second,
	}

	drenado := make(chan struct{})
	go func() {
		defer close(drenado)
		<-ctx.Done()

		apagado, cancelar := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancelar()
		if err := srv.Shutdown(apagado); err != nil {
			slog.Error("apagado sucio", "error", err)
		}

		notif.Esperar(apagado)
	}()

	slog.Info("api escuchando", "puerto", cfg.Puerto)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	<-drenado
	slog.Info("api detenida")
	return nil
}
