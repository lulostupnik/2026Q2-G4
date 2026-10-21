package cors

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func handlerDePrueba() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestApagadoNoTocaNada(t *testing.T) {
	p := Nueva("off")
	if p.Activa() {
		t.Fatal("con off no deberia activarse CORS")
	}

	grabadora := httptest.NewRecorder()
	pedido := httptest.NewRequest(http.MethodGet, "/health", nil)
	pedido.Header.Set("Origin", "https://cualquiera.com")
	p.Envolver(handlerDePrueba()).ServeHTTP(grabadora, pedido)

	if v := grabadora.Header().Get("Access-Control-Allow-Origin"); v != "" {
		t.Errorf("apagado y mando Allow-Origin: %q", v)
	}
}

func TestComodinAceptaCualquierOrigen(t *testing.T) {
	p := Nueva("*")
	if !p.Activa() {
		t.Fatal(`"*" tendria que activar CORS`)
	}

	for _, origen := range []string{"http://localhost:5173", "https://cualquiera.com"} {
		grabadora := httptest.NewRecorder()
		pedido := httptest.NewRequest(http.MethodPost, "/transferencias", nil)
		pedido.Header.Set("Origin", origen)
		p.Envolver(handlerDePrueba()).ServeHTTP(grabadora, pedido)

		if dado := grabadora.Header().Get("Access-Control-Allow-Origin"); dado != origen {
			t.Errorf("origen %q: Allow-Origin %q", origen, dado)
		}
		if grabadora.Header().Get("Vary") != "Origin" {
			t.Errorf("origen %q: falta Vary: Origin, un cache podria mezclar respuestas", origen)
		}
	}
}

func TestNoReflejaUnOrigenAjeno(t *testing.T) {
	p := Nueva("http://localhost:5173")

	casos := []struct {
		origen   string
		permitir bool
	}{
		{"http://localhost:5173", true},
		{"http://localhost:5174", false},
		{"https://sitio-malicioso.com", false},
		{"", false},

		{"http://localhost:5173.malicioso.com", false},
	}
	for _, c := range casos {
		grabadora := httptest.NewRecorder()
		pedido := httptest.NewRequest(http.MethodPost, "/transferencias", nil)
		if c.origen != "" {
			pedido.Header.Set("Origin", c.origen)
		}
		p.Envolver(handlerDePrueba()).ServeHTTP(grabadora, pedido)

		dado := grabadora.Header().Get("Access-Control-Allow-Origin")
		if c.permitir && dado != c.origen {
			t.Errorf("origen %q: Allow-Origin %q, se queria %q", c.origen, dado, c.origen)
		}
		if !c.permitir && dado != "" {
			t.Errorf("origen %q: no deberia autorizarse, mando %q", c.origen, dado)
		}
	}
}

func TestPreflightDeclaraLasCabecerasPropias(t *testing.T) {
	p := Nueva("http://localhost:5173")

	grabadora := httptest.NewRecorder()
	pedido := httptest.NewRequest(http.MethodOptions, "/transferencias", nil)
	pedido.Header.Set("Origin", "http://localhost:5173")
	pedido.Header.Set("Access-Control-Request-Method", "POST")
	p.Envolver(handlerDePrueba()).ServeHTTP(grabadora, pedido)

	if grabadora.Code != http.StatusNoContent {
		t.Fatalf("preflight devolvio %d, se queria 204", grabadora.Code)
	}
	permitidas := grabadora.Header().Get("Access-Control-Allow-Headers")
	for _, c := range []string{"Idempotency-Key", "X-Admin-Key", "X-Mock-Key", "Content-Type"} {
		if !contiene(permitidas, c) {
			t.Errorf("el preflight no declara %q: %q", c, permitidas)
		}
	}
}

func TestPreflightAjenoNoPasa(t *testing.T) {
	p := Nueva("http://localhost:5173")

	grabadora := httptest.NewRecorder()
	pedido := httptest.NewRequest(http.MethodOptions, "/transferencias", nil)
	pedido.Header.Set("Origin", "https://sitio-malicioso.com")
	pedido.Header.Set("Access-Control-Request-Method", "POST")
	p.Envolver(handlerDePrueba()).ServeHTTP(grabadora, pedido)

	if grabadora.Code != http.StatusForbidden {
		t.Errorf("un preflight de origen no permitido devolvio %d, se queria 403", grabadora.Code)
	}
}

func contiene(lista, valor string) bool {
	for i := 0; i+len(valor) <= len(lista); i++ {
		if lista[i:i+len(valor)] == valor {
			return true
		}
	}
	return false
}
