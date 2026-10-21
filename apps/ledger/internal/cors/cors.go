package cors

import (
	"net/http"
	"strings"
)

const cabecerasPermitidas = "Content-Type, Authorization, Idempotency-Key, X-Admin-Key, X-Mock-Key"

const metodosPermitidos = "GET, POST, PATCH, DELETE, OPTIONS"

const segundosCache = "600"

type Politica struct {
	origenes map[string]bool

	todos bool
}

func Nueva(lista string) Politica {
	p := Politica{origenes: map[string]bool{}}
	if strings.TrimSpace(lista) == "off" {
		return p
	}
	for _, o := range strings.Split(lista, ",") {
		o = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(o), "/"))
		if o == "" {
			continue
		}
		if o == "*" {
			p.todos = true
			continue
		}
		p.origenes[o] = true
	}
	return p
}

func (p Politica) Activa() bool { return p.todos || len(p.origenes) > 0 }

func (p Politica) Origenes() []string {
	if p.todos {
		return []string{"*"}
	}
	lista := make([]string, 0, len(p.origenes))
	for o := range p.origenes {
		lista = append(lista, o)
	}
	return lista
}

func (p Politica) permite(origen string) bool {
	return origen != "" && (p.todos || p.origenes[origen])
}

func (p Politica) Envolver(siguiente http.Handler) http.Handler {
	if !p.Activa() {
		return siguiente
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origen := r.Header.Get("Origin")

		if p.permite(origen) {

			w.Header().Set("Access-Control-Allow-Origin", origen)
			w.Header().Add("Vary", "Origin")
		}

		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {

			if !p.permite(origen) {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.Header().Set("Access-Control-Allow-Methods", metodosPermitidos)
			w.Header().Set("Access-Control-Allow-Headers", cabecerasPermitidas)
			w.Header().Set("Access-Control-Max-Age", segundosCache)
			w.WriteHeader(http.StatusNoContent)
			return
		}

		siguiente.ServeHTTP(w, r)
	})
}
