package archivado

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/parquet-go/parquet-go"
	"golang.org/x/sync/errgroup"

	"ledger/internal/objetos"
)

const TipoParquet = "application/vnd.apache.parquet"

type Fila struct {
	ID            int64     `parquet:"id"`
	OrdenID       int64     `parquet:"orden_id"`
	CuentaID      int64     `parquet:"cuenta_id"`
	ContraparteID int64     `parquet:"contraparte_id"`
	MontoCentavos int64     `parquet:"monto_centavos"`
	Tramo         string    `parquet:"tramo,dict"`
	CreadoEn      time.Time `parquet:"creado_en,timestamp(microsecond:utc)"`
}

func PrefijoDe(periodo string) string {
	return fmt.Sprintf("%speriodo=%s/", prefijoArchivo, periodo)
}

func ClaveParte(periodo string, parte int) string {
	return fmt.Sprintf("%sparte-%03d.parquet", PrefijoDe(periodo), parte)
}

func nuevoEscritor(w io.Writer, filasPorBloque int64) *parquet.GenericWriter[Fila] {
	return parquet.NewGenericWriter[Fila](w,
		parquet.MaxRowsPerRowGroup(filasPorBloque),
		parquet.Compression(&parquet.Zstd),
		parquet.SortingWriterConfig(parquet.SortingColumns(
			parquet.Ascending("cuenta_id"), parquet.Ascending("creado_en"), parquet.Ascending("id"))),
	)
}

const tamanioCola = 64 << 10

type Lector struct {
	obj objetos.Almacen

	mu    sync.Mutex
	colas map[string][]byte
}

func NuevoLector(obj objetos.Almacen) *Lector {
	return &Lector{obj: obj, colas: map[string][]byte{}}
}

const maxColas = 256

func (l *Lector) cola(ctx context.Context, clave string, bytes int64) ([]byte, error) {
	llave := fmt.Sprintf("%s#%d", clave, bytes)
	l.mu.Lock()
	c, ok := l.colas[llave]
	l.mu.Unlock()
	if ok {
		return c, nil
	}

	c, err := l.obj.LeerRango(ctx, clave, max(0, bytes-tamanioCola), bytes)
	if err != nil {
		return nil, err
	}

	l.mu.Lock()
	if len(l.colas) >= maxColas {
		clear(l.colas)
	}
	l.colas[llave] = c
	l.mu.Unlock()
	return c, nil
}

const tamanioLectura = 4 << 10

type tramo struct {
	datos []byte
	desde int64
}

func (t tramo) cubre(off int64, n int) bool {
	return off >= t.desde && off+int64(n) <= t.desde+int64(len(t.datos))
}

type lectorRangos struct {
	ctx   context.Context
	obj   objetos.Almacen
	clave string

	cola     tramo
	ventanas []tramo

	pedidos int
	bytes   int64
}

func (r *lectorRangos) ReadAt(p []byte, off int64) (int, error) {
	for _, t := range r.ventanas {
		if t.cubre(off, len(p)) {
			return copy(p, t.datos[off-t.desde:]), nil
		}
	}
	if r.cola.cubre(off, len(p)) {
		return copy(p, r.cola.datos[off-r.cola.desde:]), nil
	}
	r.pedidos++
	datos, err := r.obj.LeerRango(r.ctx, r.clave, off, off+int64(len(p)))
	if err != nil {
		return 0, err
	}
	r.bytes += int64(len(datos))
	n := copy(p, datos)
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (r *lectorRangos) precargar(rangos [][2]int64) error {
	slices.SortFunc(rangos, func(a, b [2]int64) int { return cmp.Compare(a[0], b[0]) })
	var unidos [][2]int64
	for _, rg := range rangos {
		if n := len(unidos); n > 0 && rg[0] <= unidos[n-1][1] {
			unidos[n-1][1] = max(unidos[n-1][1], rg[1])
			continue
		}
		unidos = append(unidos, rg)
	}

	ventanas := make([]tramo, len(unidos))
	g, gctx := errgroup.WithContext(r.ctx)
	g.SetLimit(4)
	for i, rg := range unidos {
		g.Go(func() error {
			datos, err := r.obj.LeerRango(gctx, r.clave, rg[0], rg[1])
			ventanas[i] = tramo{datos: datos, desde: rg[0]}
			return err
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	r.ventanas = ventanas
	r.pedidos += len(ventanas)
	for _, v := range ventanas {
		r.bytes += int64(len(v.datos))
	}
	return nil
}

type Lectura struct {
	Bloques       int
	BloquesLeidos int
	Pedidos       int
	Bytes         int64
}

var errCorte = errors.New("corte")

func (l *Lector) abrir(ctx context.Context, clave string, bytes int64, conIndice bool) (*parquet.File, *lectorRangos, error) {
	cola, err := l.cola(ctx, clave, bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("leer el pie de %s: %w", clave, err)
	}
	r := &lectorRangos{ctx: ctx, obj: l.obj, clave: clave, cola: tramo{datos: cola, desde: bytes - int64(len(cola))}}
	f, err := parquet.OpenFile(r, bytes,
		parquet.SkipMagicBytes(true),
		parquet.SkipPageIndex(!conIndice),
		parquet.SkipBloomFilters(true),
		parquet.ReadBufferSize(tamanioLectura),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("abrir %s: %w", clave, err)
	}
	return f, r, nil
}

func (l *Lector) Recorrer(ctx context.Context, clave string, bytes int64, cuenta *int64, fn func(Fila) error) (lect Lectura, err error) {
	f, r, err := l.abrir(ctx, clave, bytes, cuenta != nil)
	if err != nil {
		return lect, err
	}

	columna, ok := f.Schema().Lookup("cuenta_id")
	if !ok {
		return lect, fmt.Errorf("%s no tiene la columna cuenta_id", clave)
	}

	grupos := f.RowGroups()
	lect.Bloques = len(grupos)
	defer func() { lect.Pedidos, lect.Bytes = r.pedidos, r.bytes }()

	for i, g := range grupos {
		desde, hasta := int64(0), g.NumRows()
		rangos := [][2]int64{rangoDelBloque(f, i)}
		if cuenta != nil {
			trozo, ok := g.ColumnChunks()[columna.ColumnIndex].(*parquet.FileColumnChunk)
			if !ok {
				return lect, fmt.Errorf("%s: el bloque %d no es de un archivo", clave, i)
			}
			minimo, maximo, conLimites := trozo.Bounds()
			if conLimites && *cuenta < minimo.Int64() {
				break
			}
			if conLimites && *cuenta > maximo.Int64() {
				continue
			}
			var esta bool
			desde, hasta, rangos, esta = paginasDeCuenta(f, i, columna.ColumnIndex, *cuenta)
			if !esta {
				continue
			}
		}

		if err := r.precargar(rangos); err != nil {
			return lect, fmt.Errorf("leer el bloque %d de %s: %w", i, clave, err)
		}
		lect.BloquesLeidos++

		err = recorrerBloque(g, desde, hasta, cuenta, fn)
		if errors.Is(err, errCorte) {
			return lect, nil
		}
		if err != nil {
			return lect, fmt.Errorf("leer el bloque %d de %s: %w", i, clave, err)
		}
	}
	return lect, nil
}

func rangoDelBloque(f *parquet.File, i int) [2]int64 {
	desde, hasta := int64(-1), int64(0)
	for _, c := range f.Metadata().RowGroups[i].Columns {
		inicio := c.MetaData.DataPageOffset
		if c.MetaData.DictionaryPageOffset > 0 && c.MetaData.DictionaryPageOffset < inicio {
			inicio = c.MetaData.DictionaryPageOffset
		}
		if desde < 0 || inicio < desde {
			desde = inicio
		}
		hasta = max(hasta, inicio+c.MetaData.TotalCompressedSize)
	}
	return [2]int64{desde, hasta}
}

// Cada rango se estira tamanioLectura bytes porque parquet-go lee de a buffers de ese tamaño:
// así ninguna lectura cae fuera de lo precargado.
func paginasDeCuenta(f *parquet.File, i, columna int, cuenta int64) (int64, int64, [][2]int64, bool) {
	grupo := f.RowGroups()[i]
	trozos := grupo.ColumnChunks()
	indice, err := trozos[columna].ColumnIndex()
	if err != nil {
		return 0, grupo.NumRows(), [][2]int64{rangoDelBloque(f, i)}, true
	}
	ubicaciones, err := trozos[columna].OffsetIndex()
	if err != nil {
		return 0, grupo.NumRows(), [][2]int64{rangoDelBloque(f, i)}, true
	}

	primera, ultima := -1, -1
	for p := range indice.NumPages() {
		if indice.NullPage(p) || indice.MaxValue(p).Int64() < cuenta {
			continue
		}
		if indice.MinValue(p).Int64() > cuenta {
			break
		}
		if primera < 0 {
			primera = p
		}
		ultima = p
	}
	if primera < 0 {
		return 0, 0, nil, false
	}
	desde, hasta := ubicaciones.FirstRowIndex(primera), grupo.NumRows()
	if ultima+1 < ubicaciones.NumPages() {
		hasta = ubicaciones.FirstRowIndex(ultima + 1)
	}

	var rangos [][2]int64
	for c, meta := range f.Metadata().RowGroups[i].Columns {
		datos, diccionario := meta.MetaData.DataPageOffset, meta.MetaData.DictionaryPageOffset
		finTrozo := datos + meta.MetaData.TotalCompressedSize
		if diccionario > 0 {
			finTrozo = diccionario + meta.MetaData.TotalCompressedSize
			rangos = append(rangos, [2]int64{diccionario, min(datos+tamanioLectura, finTrozo)})
		}

		paginas, err := trozos[c].OffsetIndex()
		if err != nil {
			return 0, grupo.NumRows(), [][2]int64{rangoDelBloque(f, i)}, true
		}
		a := sort.Search(paginas.NumPages(), func(p int) bool { return paginas.FirstRowIndex(p) > desde }) - 1
		b := sort.Search(paginas.NumPages(), func(p int) bool { return paginas.FirstRowIndex(p) >= hasta }) - 1
		fin := paginas.Offset(b) + paginas.CompressedPageSize(b) + tamanioLectura
		rangos = append(rangos, [2]int64{paginas.Offset(a), min(fin, finTrozo)})
	}
	return desde, hasta, rangos, true
}

func recorrerBloque(g parquet.RowGroup, desde, hasta int64, cuenta *int64, fn func(Fila) error) error {
	lector := parquet.NewGenericRowGroupReader[Fila](g)
	defer lector.Close()
	if desde > 0 {
		if err := lector.SeekToRow(desde); err != nil {
			return err
		}
	}

	lote := make([]Fila, 1024)
	for restantes := hasta - desde; restantes > 0; {
		n, err := lector.Read(lote[:min(int64(len(lote)), restantes)])
		restantes -= int64(n)
		for _, fila := range lote[:n] {
			if cuenta != nil {
				if fila.CuentaID < *cuenta {
					continue
				}
				if fila.CuentaID > *cuenta {
					return errCorte
				}
			}
			if err := fn(fila); err != nil {
				return err
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (l *Lector) ContarFilas(ctx context.Context, clave string, bytes int64) (int64, error) {
	f, _, err := l.abrir(ctx, clave, bytes, false)
	if err != nil {
		return 0, err
	}
	return f.NumRows(), nil
}
