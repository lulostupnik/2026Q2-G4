package ledger_test

import (
	"context"
	"errors"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"ledger/db"
	"ledger/internal/apierr"
	"ledger/internal/archivado"
	"ledger/internal/ledger"
	"ledger/internal/objetos"
	"ledger/internal/pg"
	"ledger/internal/proveedor"
)

const varURL = "DB_URL_TEST"

var periodoViejo = pg.InicioDeMes(time.Now()).AddDate(0, -3, 0)

func entorno(t *testing.T) (*ledger.Servicio, *archivado.Archivador, *pgxpool.Pool) {
	t.Helper()

	url := os.Getenv(varURL)
	if url == "" {
		t.Skipf("sin %s: se saltea la prueba de integracion", varURL)
	}
	ctx := context.Background()

	pool, err := pg.Abrir(ctx, url)
	if err != nil {
		t.Fatalf("abrir la base: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatalf("limpiar el esquema: %v", err)
	}
	if _, err := db.Aplicar(ctx, pool); err != nil {
		t.Fatalf("migrar: %v", err)
	}
	if err := pg.AsegurarParticiones(ctx, pool, -6, 1); err != nil {
		t.Fatalf("crear particiones: %v", err)
	}

	alm, err := objetos.Desde(ctx, "", "", t.TempDir())
	if err != nil {
		t.Fatalf("almacen: %v", err)
	}
	svc, err := ledger.Nuevo(ctx, pool, proveedor.Nuevo("http://localhost:1", time.Second), alm, nil, time.Minute, "http://localhost:1")
	if err != nil {
		t.Fatalf("servicio: %v", err)
	}
	return svc, archivado.Nuevo(pool, alm, 0), pool
}

func TestArchivadoConservaElExtractoYElSaldo(t *testing.T) {
	svc, arch, pool := entorno(t)
	ctx := context.Background()

	origen, err := svc.CrearCuenta(ctx, ledger.EntradaCuenta{
		Alias: "ana", Tipo: "usuario", SaldoInicialCentavos: 100000})
	if err != nil {
		t.Fatalf("crear origen: %v", err)
	}
	destino, err := svc.CrearCuenta(ctx, ledger.EntradaCuenta{Alias: "kiosco", Tipo: "comercio"})
	if err != nil {
		t.Fatalf("crear destino: %v", err)
	}

	etiqueta := periodoViejo.Format("2006-01")
	if _, err := svc.SembrarPeriodo(ctx, ledger.EntradaSemilla{
		Periodo: etiqueta, OrigenID: origen.ID, DestinoID: destino.ID,
		Cantidad: 4, MontoCentavos: 250,
	}); err != nil {
		t.Fatalf("sembrar: %v", err)
	}

	antes, err := svc.MovimientosDePeriodo(ctx, origen.ID, periodoViejo)
	if err != nil {
		t.Fatalf("extracto antes: %v", err)
	}
	if antes.Origen != ledger.OrigenBase {
		t.Fatalf("antes de archivar el origen tendria que ser %q, es %q", ledger.OrigenBase, antes.Origen)
	}
	if len(antes.Movimientos) != 4 {
		t.Fatalf("antes de archivar hay %d movimientos, se querian 4", len(antes.Movimientos))
	}
	saldoAntes, err := svc.Saldo(ctx, origen.ID)
	if err != nil {
		t.Fatalf("saldo antes: %v", err)
	}

	if _, err := arch.Agendar(ctx); err != nil {
		t.Fatalf("agendar: %v", err)
	}
	res, err := arch.Ciclo(ctx)
	if err != nil {
		t.Fatalf("ciclo: %v", err)
	}
	if res == nil || res.Reprogramada {
		t.Fatalf("el archivado no corrio: %+v", res)
	}

	var quedaParticion bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`,
		"public."+pg.NombreParticion(pg.InicioDeMes(periodoViejo))).Scan(&quedaParticion); err != nil {
		t.Fatal(err)
	}
	if quedaParticion {
		t.Fatal("la particion sigue en la base despues de archivar")
	}

	despues, err := svc.MovimientosDePeriodo(ctx, origen.ID, periodoViejo)
	if err != nil {
		t.Fatalf("extracto despues: %v", err)
	}
	if despues.Origen != ledger.OrigenArchivo {
		t.Fatalf("despues de archivar el origen tendria que ser %q, es %q", ledger.OrigenArchivo, despues.Origen)
	}
	if len(despues.Movimientos) != len(antes.Movimientos) {
		t.Fatalf("el archivo tiene %d movimientos y la base tenia %d",
			len(despues.Movimientos), len(antes.Movimientos))
	}
	for i := range antes.Movimientos {
		if !mismoMovimiento(despues.Movimientos[i], antes.Movimientos[i]) {
			t.Errorf("movimiento %d cambio al archivarse:\n  base    %+v\n  archivo %+v",
				i, antes.Movimientos[i], despues.Movimientos[i])
		}
	}

	saldoDespues, err := svc.Saldo(ctx, origen.ID)
	if err != nil {
		t.Fatalf("saldo despues: %v", err)
	}
	if !saldoDespues.Coincide {
		t.Errorf("el saldo dejo de cuadrar tras archivar: vigente %d, derivado %d",
			saldoDespues.VigenteCentavos, saldoDespues.DerivadoCentavos)
	}
	if saldoDespues.VigenteCentavos != saldoAntes.VigenteCentavos {
		t.Errorf("el saldo cambio al archivar: antes %d, despues %d",
			saldoAntes.VigenteCentavos, saldoDespues.VigenteCentavos)
	}

	cierre, err := svc.VerificarCierre(ctx)
	if err != nil {
		t.Fatalf("cierre: %v", err)
	}
	if !cierre.Cierra {
		t.Errorf("el cierre no da: %d descuadres", len(cierre.Descuadres))
	}
}

func TestExportacionContableDistingueLasCuentas(t *testing.T) {
	svc, _, _ := entorno(t)
	ctx := context.Background()

	a, err := svc.CrearCuenta(ctx, ledger.EntradaCuenta{Alias: "a", Tipo: "usuario", SaldoInicialCentavos: 10000})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.CrearCuenta(ctx, ledger.EntradaCuenta{Alias: "b", Tipo: "usuario"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Transferir(ctx, ledger.EntradaTransferencia{
		IdempotencyKey: "e2e-1", OrigenID: a.ID, DestinoID: b.ID, MontoCentavos: 500,
	}); err != nil {
		t.Fatal(err)
	}

	mes := pg.InicioDeMes(time.Now()).Format("2006-01")
	exp, err := svc.Exportar(ctx, ledger.EntradaExportacion{Desde: mes, Hasta: mes})
	if err != nil {
		t.Fatalf("exportar: %v", err)
	}
	if exp.Estado != ledger.ExportLista {
		t.Fatalf("estado %q, se queria %q", exp.Estado, ledger.ExportLista)
	}
	if exp.Filas == nil || *exp.Filas == 0 {
		t.Fatal("la exportacion salio vacia")
	}

	for _, c := range []*ledger.Cuenta{a, b} {
		ext, err := svc.MovimientosDePeriodo(ctx, c.ID, time.Now())
		if err != nil {
			t.Fatalf("extracto de %s: %v", c.Alias, err)
		}
		for _, m := range ext.Movimientos {
			if m.CuentaID != c.ID {
				t.Errorf("el extracto de %s trae un movimiento de la cuenta %d", c.Alias, m.CuentaID)
			}
		}
	}
}

func TestReintentoConOtraOperacionEsConflicto(t *testing.T) {
	svc, _, _ := entorno(t)
	ctx := context.Background()

	cuenta := func(alias string) int64 {
		c, err := svc.CrearCuenta(ctx, ledger.EntradaCuenta{
			Alias: alias, Tipo: "usuario", SaldoInicialCentavos: 100000})
		if err != nil {
			t.Fatalf("crear %s: %v", alias, err)
		}
		return c.ID
	}
	ana, beto, caro := cuenta("ana"), cuenta("beto"), cuenta("caro")

	primera, err := svc.Transferir(ctx, ledger.EntradaTransferencia{
		IdempotencyKey: "clave-1", OrigenID: ana, DestinoID: beto, MontoCentavos: 500})
	if err != nil {
		t.Fatalf("primera transferencia: %v", err)
	}

	repetida, err := svc.Transferir(ctx, ledger.EntradaTransferencia{
		IdempotencyKey: "clave-1", OrigenID: ana, DestinoID: beto, MontoCentavos: 500})
	if err != nil || repetida.ID != primera.ID {
		t.Fatalf("el reintento tenia que devolver la orden %d, devolvio %+v, %v", primera.ID, repetida, err)
	}

	ajena, err := svc.Transferir(ctx, ledger.EntradaTransferencia{
		IdempotencyKey: "clave-1", OrigenID: caro, DestinoID: beto, MontoCentavos: 500})
	var e *apierr.Error
	if !errors.As(err, &e) || e.Estado != http.StatusUnprocessableEntity {
		t.Fatalf("tenia que ser 422, fue %+v, %v", ajena, err)
	}
	if ajena != nil {
		t.Fatalf("un 422 no puede devolver la orden: %+v", ajena)
	}

	if _, err := svc.Transferir(ctx, ledger.EntradaTransferencia{
		IdempotencyKey: "clave-1", OrigenID: ana, DestinoID: beto, MontoCentavos: 999}); !errors.As(err, &e) || e.Estado != http.StatusUnprocessableEntity {
		t.Fatalf("otro monto con la misma clave tenia que ser 422, fue %v", err)
	}
}

type almacenQueFalla struct {
	objetos.Almacen
	fallas int
}

func (a *almacenQueFalla) Guardar(ctx context.Context, clave string, cuerpo []byte, tipo string) (string, error) {
	if a.fallas > 0 {
		a.fallas--
		return "", errors.New("PUT cortado")
	}
	return a.Almacen.Guardar(ctx, clave, cuerpo, tipo)
}

func TestArchivadoSeRecuperaSiFallaLaExportacion(t *testing.T) {
	svc, _, pool := entorno(t)
	ctx := context.Background()

	disco, err := objetos.Desde(ctx, "", "", t.TempDir())
	if err != nil {
		t.Fatalf("almacen: %v", err)
	}
	arch := archivado.Nuevo(pool, &almacenQueFalla{Almacen: disco, fallas: 1}, 0)

	origen, err := svc.CrearCuenta(ctx, ledger.EntradaCuenta{Alias: "ana", Tipo: "usuario", SaldoInicialCentavos: 100000})
	if err != nil {
		t.Fatalf("crear origen: %v", err)
	}
	destino, err := svc.CrearCuenta(ctx, ledger.EntradaCuenta{Alias: "kiosco", Tipo: "comercio"})
	if err != nil {
		t.Fatalf("crear destino: %v", err)
	}
	if _, err := svc.SembrarPeriodo(ctx, ledger.EntradaSemilla{
		Periodo: periodoViejo.Format("2006-01"), OrigenID: origen.ID, DestinoID: destino.ID,
		Cantidad: 3, MontoCentavos: 100,
	}); err != nil {
		t.Fatalf("sembrar: %v", err)
	}
	if _, err := arch.Agendar(ctx); err != nil {
		t.Fatalf("agendar: %v", err)
	}

	if _, err := arch.Ciclo(ctx); err == nil {
		t.Fatal("la primera corrida tenia que fallar en la exportacion")
	}

	if _, err := pool.Exec(ctx, `UPDATE tareas SET proxima_ejecucion = now()`); err != nil {
		t.Fatal(err)
	}

	res, err := arch.Ciclo(ctx)
	if err != nil {
		t.Fatalf("el reintento tenia que terminar el archivado: %v", err)
	}
	if res == nil || res.Filas == 0 {
		t.Fatalf("el reintento no exporto nada: %+v", res)
	}
	var estado string
	if err := pool.QueryRow(ctx, `SELECT estado FROM tareas WHERE tipo = 'archivado'`).Scan(&estado); err != nil {
		t.Fatal(err)
	}
	if estado != "hecha" {
		t.Fatalf("la tarea quedo en %q, se esperaba hecha", estado)
	}
}

func mismoMovimiento(a, b ledger.Movimiento) bool {
	periodo, creado := a.Periodo, a.CreadoEn
	a.Periodo, a.CreadoEn = b.Periodo, b.CreadoEn
	return a == b && periodo.Equal(b.Periodo) && creado.Equal(b.CreadoEn)
}
