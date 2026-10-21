package db

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed *.sql
var archivos embed.FS

const idLock = 8208_2025

const tablaHistorial = `
CREATE TABLE IF NOT EXISTS migraciones (
    version     TEXT PRIMARY KEY,
    aplicada_en TIMESTAMPTZ NOT NULL DEFAULT now()
)`

const columnaChecksum = `ALTER TABLE migraciones ADD COLUMN IF NOT EXISTS checksum TEXT`

func Aplicar(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("tomar conexion: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", idLock); err != nil {
		return nil, fmt.Errorf("tomar advisory lock: %w", err)
	}
	defer conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", idLock)

	if _, err := conn.Exec(ctx, tablaHistorial); err != nil {
		return nil, fmt.Errorf("crear migraciones: %w", err)
	}
	if _, err := conn.Exec(ctx, columnaChecksum); err != nil {
		return nil, fmt.Errorf("agregar checksum a migraciones: %w", err)
	}

	if err := backfill(ctx, conn); err != nil {
		return nil, err
	}

	aplicadas, err := versionesAplicadas(ctx, conn)
	if err != nil {
		return nil, err
	}

	if err := verificarChecksums(aplicadas); err != nil {
		return nil, err
	}

	pendientes, err := versionesPendientes(aplicadas)
	if err != nil {
		return nil, err
	}

	for _, version := range pendientes {
		if err := aplicarUna(ctx, conn, version); err != nil {
			return nil, fmt.Errorf("aplicar %s: %w", version, err)
		}
	}
	return pendientes, nil
}

func versionesAplicadas(ctx context.Context, conn *pgxpool.Conn) (map[string]*string, error) {
	filas, err := conn.Query(ctx, "SELECT version, checksum FROM migraciones")
	if err != nil {
		return nil, fmt.Errorf("leer migraciones: %w", err)
	}
	defer filas.Close()

	aplicadas := make(map[string]*string)
	for filas.Next() {
		var v string
		var suma *string
		if err := filas.Scan(&v, &suma); err != nil {
			return nil, err
		}
		aplicadas[v] = suma
	}
	return aplicadas, filas.Err()
}

func checksumDe(version string) (string, error) {
	contenido, err := archivos.ReadFile(version)
	if err != nil {
		return "", err
	}
	suma := sha256.Sum256(contenido)
	return hex.EncodeToString(suma[:]), nil
}

func verificarChecksums(aplicadas map[string]*string) error {
	for version, guardado := range aplicadas {
		if guardado == nil {
			continue
		}
		actual, err := checksumDe(version)
		if err != nil {

			return fmt.Errorf("la migracion %s figura aplicada y no esta en el binario: "+
				"una migracion commiteada no se borra ni se renombra", version)
		}
		if actual != *guardado {
			return fmt.Errorf("la migracion %s cambio despues de haberse aplicado "+
				"(esperado %s, es %s): una migracion commiteada no se toca, "+
				"los cambios de esquema van en un archivo nuevo", version, *guardado, actual)
		}
	}
	return nil
}

func backfill(ctx context.Context, conn *pgxpool.Conn) error {
	var hayEsquema, hayHistorial bool
	if err := conn.QueryRow(ctx, `
		SELECT to_regclass('public.cuentas') IS NOT NULL,
		       EXISTS (SELECT 1 FROM migraciones)`).Scan(&hayEsquema, &hayHistorial); err != nil {
		return fmt.Errorf("verificar esquema previo: %w", err)
	}
	if !hayEsquema || hayHistorial {
		return nil
	}

	_, err := conn.Exec(ctx, `
		INSERT INTO migraciones (version) VALUES ('001_schema.sql'), ('002_cuentas_internas.sql')
		ON CONFLICT (version) DO NOTHING`)
	if err != nil {
		return fmt.Errorf("registrar migraciones previas: %w", err)
	}
	return nil
}

func versionesPendientes(aplicadas map[string]*string) ([]string, error) {
	entradas, err := archivos.ReadDir(".")
	if err != nil {
		return nil, err
	}

	pendientes := make([]string, 0, len(entradas))
	for _, e := range entradas {
		nombre := e.Name()
		if _, ya := aplicadas[nombre]; !strings.HasSuffix(nombre, ".sql") || ya {
			continue
		}
		pendientes = append(pendientes, nombre)
	}
	sort.Strings(pendientes)
	return pendientes, nil
}

func aplicarUna(ctx context.Context, conn *pgxpool.Conn, version string) error {
	sql, err := archivos.ReadFile(version)
	if err != nil {
		return err
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))

	if _, err := tx.Exec(ctx, string(sql)); err != nil {
		return err
	}
	suma, err := checksumDe(version)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		"INSERT INTO migraciones (version, checksum) VALUES ($1, $2)", version, suma); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
