package ledger

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"ledger/internal/apierr"
)

func TestEstadoHTTPSoloElEnDudaNoEsDefinitivo(t *testing.T) {
	casos := map[string]int{
		EstadoLiquidada:  http.StatusOK,
		EstadoAcreditada: http.StatusOK,
		EstadoDevuelta:   http.StatusOK,
		EstadoReservada:  http.StatusOK,
		EstadoEnDuda:     http.StatusAccepted,
	}
	for estado, esperado := range casos {
		if got := EstadoHTTP(&Orden{Estado: estado}); got != esperado {
			t.Errorf("EstadoHTTP(%s) = %d, se esperaba %d", estado, got, esperado)
		}
	}
}

func TestResultadoDeOrdenRechazadaDevuelveElMotivoGuardado(t *testing.T) {
	o := &Orden{Estado: EstadoRechazada, Motivo: "saldo 10 insuficiente para 20 centavos"}
	got, err := resultado(o)
	if got != o {
		t.Error("se esperaba que devolviera la orden igual")
	}
	var e *apierr.Error
	if !errors.As(err, &e) {
		t.Fatalf("se esperaba un *apierr.Error, vino %v", err)
	}
	if e.Estado != http.StatusUnprocessableEntity {
		t.Errorf("estado = %d", e.Estado)
	}
	if e.Mensaje != o.Motivo {
		t.Errorf("el mensaje tiene que ser el motivo guardado, vino %q", e.Mensaje)
	}
}

func TestResultadoDeOrdenLiquidadaNoEsError(t *testing.T) {
	if _, err := resultado(&Orden{Estado: EstadoLiquidada}); err != nil {
		t.Errorf("no se esperaba error: %v", err)
	}
}

func TestNuevaReferenciaEsUnicaYLegible(t *testing.T) {
	vistas := map[string]bool{}
	for i := 0; i < 1000; i++ {
		r := nuevaReferencia("ING")
		if !strings.HasPrefix(r, "ING-") {
			t.Fatalf("referencia sin prefijo: %s", r)
		}
		if strings.ContainsAny(r[4:], "=") {
			t.Fatalf("la referencia no deberia tener relleno: %s", r)
		}
		if vistas[r] {
			t.Fatalf("referencia repetida en 1000 intentos: %s", r)
		}
		vistas[r] = true
	}
}

func TestNuloSiVacio(t *testing.T) {
	if nuloSiVacio("") != nil {
		t.Error("la cadena vacia tiene que ir a la base como NULL")
	}
	if nuloSiVacio("ING-1") != "ING-1" {
		t.Error("una cadena con contenido tiene que pasar tal cual")
	}
}
