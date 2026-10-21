package webhook

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFirmarEsSobreLosBytesEnviados(t *testing.T) {
	a := Aviso{OrdenID: 7, Tipo: "transferencia", Estado: "liquidada", MontoCentavos: 100}

	cuerpo, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := Firmar(cuerpo, "s3cr3t"), Firmar(cuerpo, "s3cr3t"); got != want {
		t.Fatalf("la firma no es determinista: %q vs %q", got, want)
	}
	if Firmar(cuerpo, "s3cr3t") == Firmar(cuerpo, "otro") {
		t.Fatal("la firma no depende del secreto")
	}
	if Firmar(cuerpo, "s3cr3t") == Firmar(append(cuerpo, ' '), "s3cr3t") {
		t.Fatal("la firma no depende del cuerpo")
	}
}

func TestBackoffSeDuplica(t *testing.T) {
	n := Nuevo(nil, "http://destino", "s", 4, 100*time.Millisecond)

	esperado := []time.Duration{100, 200, 400}
	for i, quiero := range esperado {
		if got := n.esperaDe(i + 1); got != quiero*time.Millisecond {
			t.Errorf("intento %d: espera %v, se queria %v", i+1, got, quiero*time.Millisecond)
		}
	}
}

func TestSinURLNoNotifica(t *testing.T) {
	n := Nuevo(nil, "", "", 3, time.Millisecond)
	if n.Habilitado() {
		t.Fatal("sin URL no deberia estar habilitado")
	}

	n.Notificar(Aviso{OrdenID: 1, Estado: "liquidada"})
}

func TestEnviarFirmaElCuerpoYDistingueElCodigo(t *testing.T) {
	var recibida, cuerpoRecibido string
	codigo := http.StatusOK

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		recibida, cuerpoRecibido = r.Header.Get(CabeceraFirma), string(b)
		w.WriteHeader(codigo)
	}))
	defer srv.Close()

	n := Nuevo(nil, srv.URL, "s3cr3t", 3, time.Millisecond)
	cuerpo := []byte(`{"orden_id":7}`)

	if err := n.enviar(context.Background(), cuerpo, Firmar(cuerpo, "s3cr3t")); err != nil {
		t.Fatalf("un 200 no deberia dar error: %v", err)
	}
	if cuerpoRecibido != string(cuerpo) {
		t.Fatalf("el cuerpo llego alterado: %q", cuerpoRecibido)
	}

	if recibida != Firmar([]byte(cuerpoRecibido), "s3cr3t") {
		t.Fatalf("la firma no corresponde al cuerpo recibido: %q", recibida)
	}

	codigo = http.StatusInternalServerError
	if err := n.enviar(context.Background(), cuerpo, recibida); err == nil {
		t.Fatal("un 500 tiene que contar como fracaso para que se reintente")
	}
}
