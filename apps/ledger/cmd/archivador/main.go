package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ledger/db"
	"ledger/internal/archivado"
	"ledger/internal/config"
	"ledger/internal/objetos"
	"ledger/internal/pg"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := correr(); err != nil {
		slog.Error("el archivador termino con error", "error", err)
		os.Exit(1)
	}
}

func correr() error {
	cfg, err := config.Cargar()
	if err != nil {
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

	alm, err := objetos.Desde(arranque, cfg.Region, cfg.Bucket, cfg.ObjetosDir)
	if err != nil {
		return err
	}
	arch := archivado.Nuevo(pool, alm, cfg.RetencionMeses)

	slog.Info("archivador en marcha",
		"intervalo", cfg.ArchivadoIntervalo.String(), "retencion_meses", cfg.RetencionMeses)

	tic := time.NewTicker(cfg.ArchivadoIntervalo)
	defer tic.Stop()

	particiones := time.NewTicker(24 * time.Hour)
	defer particiones.Stop()

	if err := pg.AsegurarParticiones(ctx, pool, -1, 1); err != nil {
		return err
	}

	agendar(ctx, arch)

	for {
		select {
		case <-ctx.Done():

			slog.Info("archivador detenido")
			return nil

		case <-particiones.C:
			if err := pg.AsegurarParticiones(ctx, pool, -1, 1); err != nil {
				slog.Error("asegurar particiones", "error", err)
			}

		case <-tic.C:
			res, err := arch.Ciclo(ctx)
			if err != nil {
				slog.Error("ciclo de archivado", "error", err)
				continue
			}
			if res == nil {
				continue
			}
			slog.Info("archivado",
				"periodo", res.Periodo, "particion", res.Particion, "filas", res.Filas,
				"suma_centavos", res.SumaCentavos, "objeto", res.Objeto,
				"reprogramada", res.Reprogramada, "motivo", res.Motivo)
		}
	}
}

func agendar(ctx context.Context, arch *archivado.Archivador) {
	periodo, err := arch.Agendar(ctx)
	switch {
	case err != nil:
		slog.Error("agendar archivado", "error", err)
	case periodo != "":
		slog.Info("archivado agendado", "periodo", periodo)
	}
}
