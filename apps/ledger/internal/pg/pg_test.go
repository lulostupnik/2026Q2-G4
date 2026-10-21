package pg

import (
	"testing"
	"time"
)

func TestInicioDeMesNormalizaAlPrimerDiaEnUTC(t *testing.T) {
	casos := []struct {
		entrada  time.Time
		esperado string
	}{
		{time.Date(2026, 9, 7, 15, 4, 5, 0, time.UTC), "2026-09-01"},
		{time.Date(2026, 1, 31, 23, 59, 59, 0, time.UTC), "2026-01-01"},

		{time.Date(2026, 6, 30, 21, 0, 0, 0, time.FixedZone("ART", -3*3600)), "2026-07-01"},
	}
	for _, c := range casos {
		if got := InicioDeMes(c.entrada).Format("2006-01-02"); got != c.esperado {
			t.Errorf("InicioDeMes(%s) = %s, se esperaba %s", c.entrada, got, c.esperado)
		}
	}
}

func TestNombreParticion(t *testing.T) {
	p := InicioDeMes(time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC))
	if got := NombreParticion(p); got != "asientos_2026_07" {
		t.Errorf("NombreParticion = %s", got)
	}
}

func TestEsViolacionUnicaConErrorAjeno(t *testing.T) {
	if EsViolacionUnica(nil, "") {
		t.Error("nil no es una violacion de indice unico")
	}
	if EsViolacionUnica(errCualquiera{}, "ordenes_tipo_idempotency_key_key") {
		t.Error("un error que no viene de PostgreSQL no puede tomarse por una violacion")
	}
}

type errCualquiera struct{}

func (errCualquiera) Error() string { return "algo" }
