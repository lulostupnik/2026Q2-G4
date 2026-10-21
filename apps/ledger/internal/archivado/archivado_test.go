package archivado

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"testing"
	"time"
)

func TestClaveDeEsElLayoutHive(t *testing.T) {
	if got, want := ClaveDe("2026-06"), "transaction-history/periodo=2026-06/asientos.ndjson.gz"; got != want {
		t.Fatalf("clave %q, se queria %q", got, want)
	}

	if ClaveDe("2026-06") != ClaveDe("2026-06") {
		t.Fatal("la clave no es determinista")
	}
}

func TestAsientoSobreviveElFormato(t *testing.T) {
	original := Asiento{
		ID: 42, Periodo: "2026-06-01", OrdenID: 7, CuentaID: 5, ContraparteID: 6,
		MontoCentavos: -1000, Tramo: "transferencia",
		CreadoEn: time.Date(2026, 6, 4, 10, 22, 13, 0, time.UTC),
	}

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if err := json.NewEncoder(zw).Encode(original); err != nil {
		t.Fatal(err)
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	zr, err := gzip.NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	var leido Asiento
	if err := json.NewDecoder(zr).Decode(&leido); err != nil {
		t.Fatal(err)
	}

	if leido != original {
		t.Fatalf("el asiento cambio en el viaje:\n  escrito %+v\n  leido   %+v", original, leido)
	}
	if leido.CuentaID == 0 {
		t.Error("se perdio cuenta_id: sin el, la exportacion contable no distingue las cuentas")
	}
}
