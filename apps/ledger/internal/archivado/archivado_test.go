package archivado

import (
	"context"
	"io"
	"testing"
	"time"

	"ledger/internal/objetos"
)

func TestClavesSonElLayoutHive(t *testing.T) {
	if got, want := PrefijoDe("2026-06"), "transaction-history/periodo=2026-06/"; got != want {
		t.Fatalf("prefijo %q, se queria %q", got, want)
	}
	if got, want := ClaveParte("2026-06", 7), "transaction-history/periodo=2026-06/parte-007.parquet"; got != want {
		t.Fatalf("clave %q, se queria %q", got, want)
	}
}

func filasDePrueba(cuentas, porCuenta int) []Fila {
	base := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	var filas []Fila
	id := int64(1)
	for c := 1; c <= cuentas; c++ {
		for i := 0; i < porCuenta; i++ {
			filas = append(filas, Fila{
				ID: id, OrdenID: id, CuentaID: int64(c), ContraparteID: int64(c%cuentas + 1),
				MontoCentavos: int64((i + 1) * 100), Tramo: "transferencia",
				CreadoEn: base.Add(time.Duration(i) * time.Minute),
			})
			id++
		}
	}
	return filas
}

func escribirArchivo(t *testing.T, filas []Fila, filasPorBloque int64) (objetos.Almacen, objetos.Subido) {
	t.Helper()
	ctx := context.Background()
	obj, err := objetos.Desde(ctx, "", "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	subido, err := obj.Subir(ctx, ClaveParte("2026-06", 0), TipoParquet, func(w io.Writer) error {
		e := nuevoEscritor(w, filasPorBloque)
		if _, err := e.Write(filas); err != nil {
			return err
		}
		return e.Close()
	})
	if err != nil {
		t.Fatal(err)
	}
	return obj, subido
}

func TestUnaCuentaLeeSoloSusBloques(t *testing.T) {
	const cuentas, porCuenta = 50, 7
	filas := filasDePrueba(cuentas, porCuenta)
	obj, subido := escribirArchivo(t, filas, 20)
	lector := NuevoLector(obj)
	ctx := context.Background()

	for _, cuenta := range []int64{1, 3, 26, 50} {
		var leidas []Fila
		lect, err := lector.Recorrer(ctx, ClaveParte("2026-06", 0), subido.Bytes, &cuenta, func(f Fila) error {
			leidas = append(leidas, f)
			return nil
		})
		if err != nil {
			t.Fatalf("cuenta %d: %v", cuenta, err)
		}
		if len(leidas) != porCuenta {
			t.Fatalf("cuenta %d: %d filas, se querian %d", cuenta, len(leidas), porCuenta)
		}
		esperadas := filas[(cuenta-1)*porCuenta : cuenta*porCuenta]
		for i := range leidas {
			if !mismaFila(leidas[i], esperadas[i]) {
				t.Fatalf("cuenta %d, fila %d:\n  leida    %+v\n  esperada %+v", cuenta, i, leidas[i], esperadas[i])
			}
		}
		if lect.Bloques < 10 {
			t.Fatalf("el archivo tiene %d bloques: la prueba necesita varios", lect.Bloques)
		}
		if lect.BloquesLeidos > 2 {
			t.Errorf("cuenta %d: leyo %d de %d bloques, una cuenta ocupa como mucho dos", cuenta, lect.BloquesLeidos, lect.Bloques)
		}
	}
}

func mismaFila(a, b Fila) bool {
	mismoMomento := a.CreadoEn.Equal(b.CreadoEn)
	a.CreadoEn, b.CreadoEn = time.Time{}, time.Time{}
	return mismoMomento && a == b
}

func TestSinCuentaLeeTodoEnOrden(t *testing.T) {
	filas := filasDePrueba(12, 5)
	obj, subido := escribirArchivo(t, filas, 8)

	var n int
	lect, err := NuevoLector(obj).Recorrer(context.Background(), ClaveParte("2026-06", 0), subido.Bytes, nil, func(f Fila) error {
		if f.ID != filas[n].ID {
			t.Fatalf("fila %d: id %d, se queria %d", n, f.ID, filas[n].ID)
		}
		n++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != len(filas) {
		t.Fatalf("leyo %d filas de %d", n, len(filas))
	}
	if lect.BloquesLeidos != lect.Bloques {
		t.Fatalf("sin cuenta tiene que leer todos los bloques: %d de %d", lect.BloquesLeidos, lect.Bloques)
	}
}

func TestCuentaQueNoEstaNoLeeBloques(t *testing.T) {
	obj, subido := escribirArchivo(t, filasDePrueba(10, 4), 6)
	cuenta := int64(999)
	lect, err := NuevoLector(obj).Recorrer(context.Background(), ClaveParte("2026-06", 0), subido.Bytes, &cuenta, func(Fila) error {
		t.Fatal("no tendria que devolver filas")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if lect.BloquesLeidos != 0 {
		t.Fatalf("leyo %d bloques para una cuenta que no esta", lect.BloquesLeidos)
	}
}

func TestElPieSeLeeUnaSolaVez(t *testing.T) {
	obj, subido := escribirArchivo(t, filasDePrueba(30, 5), 10)
	lector := NuevoLector(obj)
	cuenta := int64(17)
	recorrer := func() Lectura {
		lect, err := lector.Recorrer(context.Background(), ClaveParte("2026-06", 0), subido.Bytes, &cuenta, func(Fila) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		return lect
	}
	recorrer()
	if segunda := recorrer(); segunda.Pedidos != segunda.BloquesLeidos {
		t.Fatalf("con el pie en cache tendria que pedir sólo los bloques: %d pedidos para %d bloques",
			segunda.Pedidos, segunda.BloquesLeidos)
	}
}

func TestUnaCuentaBajaSoloSusPaginas(t *testing.T) {
	const cuentas, porCuenta = 10000, 50
	filas := filasDePrueba(cuentas, porCuenta)
	obj, subido := escribirArchivo(t, filas, int64(len(filas)))
	lector := NuevoLector(obj)
	ctx := context.Background()

	for _, cuenta := range []int64{1, 1234, 7777, cuentas} {
		var leidas []Fila
		lect, err := lector.Recorrer(ctx, ClaveParte("2026-06", 0), subido.Bytes, &cuenta, func(f Fila) error {
			leidas = append(leidas, f)
			return nil
		})
		if err != nil {
			t.Fatalf("cuenta %d: %v", cuenta, err)
		}
		esperadas := filas[(cuenta-1)*porCuenta : cuenta*porCuenta]
		if len(leidas) != len(esperadas) {
			t.Fatalf("cuenta %d: %d filas, se querian %d", cuenta, len(leidas), len(esperadas))
		}
		for i := range leidas {
			if !mismaFila(leidas[i], esperadas[i]) {
				t.Fatalf("cuenta %d, fila %d:\n  leida    %+v\n  esperada %+v", cuenta, i, leidas[i], esperadas[i])
			}
		}
		if lect.Bloques != 1 {
			t.Fatalf("la prueba necesita un solo bloque grande y hay %d", lect.Bloques)
		}
		if lect.Bytes > subido.Bytes/4 {
			t.Errorf("cuenta %d: bajó %d bytes de un archivo de %d", cuenta, lect.Bytes, subido.Bytes)
		}
	}
}
