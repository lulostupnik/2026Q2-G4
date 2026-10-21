package ledger

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"ledger/internal/apierr"
)

type ProveedorActivo struct {
	URL           string     `json:"url"`
	PorDefecto    bool       `json:"por_defecto"`
	ActualizadoEn *time.Time `json:"actualizado_en,omitempty"`
	Origen        string     `json:"origen,omitempty"`
}

func (s *Servicio) VerProveedor(ctx context.Context) (*ProveedorActivo, error) {
	var p ProveedorActivo
	var origen *string
	err := s.pool.QueryRow(ctx,
		`SELECT url, actualizado_en, origen FROM proveedor_activo WHERE id = 1`).
		Scan(&p.URL, &p.ActualizadoEn, &origen)
	if errors.Is(err, pgx.ErrNoRows) {
		return &ProveedorActivo{URL: s.proveedorPorDefecto, PorDefecto: true}, nil
	}
	if err != nil {
		return nil, err
	}
	if origen != nil {
		p.Origen = *origen
	}
	return &p, nil
}

func (s *Servicio) CambiarProveedor(ctx context.Context, crudo, origen string) (*ProveedorActivo, error) {
	limpia, err := validarURLDeProveedor(crudo)
	if err != nil {
		return nil, err
	}

	if _, err := s.pool.Exec(ctx, `
		INSERT INTO proveedor_activo (id, url, origen) VALUES (1, $1, $2)
		ON CONFLICT (id) DO UPDATE
		SET url = EXCLUDED.url, origen = EXCLUDED.origen, actualizado_en = now()`,
		limpia, nuloSiVacio(origen)); err != nil {
		return nil, err
	}
	return s.VerProveedor(ctx)
}

func (s *Servicio) RestaurarProveedor(ctx context.Context) (*ProveedorActivo, error) {
	if _, err := s.pool.Exec(ctx, `DELETE FROM proveedor_activo WHERE id = 1`); err != nil {
		return nil, err
	}
	return s.VerProveedor(ctx)
}

func validarURLDeProveedor(crudo string) (string, error) {
	crudo = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(crudo), "/"))
	if crudo == "" {
		return "", apierr.Invalido("falta url")
	}

	u, err := url.Parse(crudo)
	if err != nil {
		return "", apierr.Invalido("url invalida: %v", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", apierr.Invalido("la url tiene que empezar con http:// o https://")
	}
	if u.Host == "" {
		return "", apierr.Invalido("la url no tiene host")
	}

	if ip := net.ParseIP(u.Hostname()); ip != nil && ip.IsLinkLocalUnicast() {
		return "", apierr.New(http.StatusForbidden, "destino_prohibido",
			"no se puede apuntar el proveedor al rango link-local (%s): ahi vive el servicio "+
				"de metadatos de la instancia", u.Hostname())
	}
	return crudo, nil
}
