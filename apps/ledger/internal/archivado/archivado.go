package archivado

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"

	"ledger/internal/objetos"
	"ledger/internal/pg"
)

const estadosTerminales = `'liquidada','rechazada','acreditada','devuelta','atribuida','no_atribuida'`

const tipoTarea = "archivado"

const prefijoArchivo = "transaction-history/"

type Archivador struct {
	pool           *pgxpool.Pool
	obj            objetos.Almacen
	lector         *Lector
	retencionMeses int

	filasPorParte  int64
	filasPorBloque int64
	paralelismo    int
}

const (
	FilasPorPartePorDefecto  = 5_000_000
	FilasPorBloquePorDefecto = 1_000_000
	ParalelismoPorDefecto    = 2
)

func Nuevo(pool *pgxpool.Pool, obj objetos.Almacen, retencionMeses int) *Archivador {
	return &Archivador{
		pool: pool, obj: obj, lector: NuevoLector(obj), retencionMeses: retencionMeses,
		filasPorParte: FilasPorPartePorDefecto, filasPorBloque: FilasPorBloquePorDefecto,
		paralelismo: ParalelismoPorDefecto,
	}
}

func (a *Archivador) ConPartes(filasPorParte, filasPorBloque int64, paralelismo int) *Archivador {
	if filasPorParte > 0 {
		a.filasPorParte = filasPorParte
	}
	if filasPorBloque > 0 {
		a.filasPorBloque = filasPorBloque
	}
	if paralelismo > 0 {
		a.paralelismo = paralelismo
	}
	return a
}

type Resultado struct {
	Periodo      string `json:"periodo"`
	Particion    string `json:"particion,omitempty"`
	Filas        int64  `json:"filas"`
	SumaCentavos int64  `json:"suma_centavos"`
	Objeto       string `json:"objeto,omitempty"`
	Clave        string `json:"clave,omitempty"`
	Partes       int    `json:"partes"`
	Corte        string `json:"corte"`
	Reprogramada bool   `json:"reprogramada"`
	Motivo       string `json:"motivo,omitempty"`
}

func (a *Archivador) Agendar(ctx context.Context) (string, error) {
	limite := pg.InicioDeMes(time.Now()).AddDate(0, -a.retencionMeses, 0)

	var periodo *time.Time
	err := a.pool.QueryRow(ctx,
		`SELECT min(periodo) FROM asientos WHERE periodo < $1`, limite).Scan(&periodo)
	if err != nil {
		return "", err
	}
	if periodo == nil {
		return "", nil
	}

	_, err = a.pool.Exec(ctx, `
		INSERT INTO tareas (tipo, periodo, proxima_ejecucion)
		VALUES ($1, $2, now())
		ON CONFLICT (tipo, periodo) DO NOTHING`,
		tipoTarea, *periodo)
	if err != nil {
		return "", err
	}
	return periodo.Format("2006-01"), nil
}

func (a *Archivador) Ciclo(ctx context.Context) (*Resultado, error) {
	var tareaID int64
	var periodo time.Time
	var res *Resultado

	err := pg.EnTx(ctx, a.pool, func(tx pgx.Tx) error {

		err := tx.QueryRow(ctx, `
			SELECT id, periodo FROM tareas
			WHERE tipo = $1 AND estado = 'pendiente' AND proxima_ejecucion <= now()
			ORDER BY proxima_ejecucion
			LIMIT 1
			FOR UPDATE SKIP LOCKED`, tipoTarea).Scan(&tareaID, &periodo)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}

		periodo = pg.InicioDeMes(periodo)
		corte := periodo.AddDate(0, 1, 0)
		res = &Resultado{Periodo: periodo.Format("2006-01"), Corte: corte.Format("2006-01")}

		var abiertas int64
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM ordenes
			WHERE creada_en < $1 AND estado::text NOT IN (`+estadosTerminales+`)`,
			corte).Scan(&abiertas); err != nil {
			return err
		}
		if abiertas > 0 {
			res.Reprogramada = true
			res.Motivo = fmt.Sprintf("el periodo no esta cerrado: %d ordenes en estado no terminal", abiertas)
			_, err := tx.Exec(ctx, `
				UPDATE tareas
				SET proxima_ejecucion = now() + interval '1 hour',
				    intentos = intentos + 1,
				    ultimo_error = $2
				WHERE id = $1`, tareaID, res.Motivo)
			return err
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO saldos_apertura (cuenta_id, corte, saldo_centavos)
			SELECT s.cuenta_id, $2, coalesce(prev.saldo_centavos, 0) + coalesce(sum(a.monto_centavos), 0)
			FROM saldos s
			LEFT JOIN saldos_apertura prev ON prev.cuenta_id = s.cuenta_id AND prev.corte = $1
			LEFT JOIN asientos a ON a.cuenta_id = s.cuenta_id AND a.periodo = $1
			GROUP BY s.cuenta_id, prev.saldo_centavos
			ON CONFLICT (cuenta_id, corte) DO NOTHING`, periodo, corte); err != nil {
			return err
		}

		particion := pg.NombreParticion(periodo)
		var existe bool
		if err := tx.QueryRow(ctx,
			`SELECT to_regclass($1) IS NOT NULL`, "public."+particion).Scan(&existe); err != nil {
			return err
		}
		if !existe {
			res.Motivo = "el periodo no tiene particion: solo se escribio el saldo de apertura"
			_, err := tx.Exec(ctx, `UPDATE tareas SET estado = 'hecha' WHERE id = $1`, tareaID)
			return err
		}
		res.Particion = particion

		if _, err := tx.Exec(ctx, `
			INSERT INTO periodos_archivados (periodo, clave, uri, filas, suma_centavos)
			VALUES ($1, NULL, NULL, NULL, NULL)
			ON CONFLICT (periodo) DO NOTHING`, periodo); err != nil {
			return err
		}

		var enganchada bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM pg_inherits
			               WHERE inhrelid = to_regclass($1) AND inhparent = 'asientos'::regclass)`,
			"public."+particion).Scan(&enganchada); err != nil {
			return err
		}
		if !enganchada {
			return nil
		}
		_, err = tx.Exec(ctx, fmt.Sprintf(`ALTER TABLE asientos DETACH PARTITION %s`, particion))
		return err
	})
	if err != nil || res == nil || res.Reprogramada || res.Particion == "" {
		return res, err
	}

	if err := a.exportarYBorrar(ctx, tareaID, res); err != nil {
		_, errTarea := a.pool.Exec(ctx, `
			UPDATE tareas SET proxima_ejecucion = now() + interval '5 minutes',
			                  intentos = intentos + 1, ultimo_error = $2
			WHERE id = $1`, tareaID, err.Error())
		if errTarea != nil {
			return res, errors.Join(err, errTarea)
		}
		return res, err
	}
	return res, nil
}

type rango struct {
	desde, hasta int64
	filas        int64
	suma         int64
}

type Parte struct {
	Numero       int
	Clave        string
	URI          string
	CuentaMin    int64
	CuentaMax    int64
	Filas        int64
	SumaCentavos int64
	Bytes        int64
}

func (a *Archivador) rangos(ctx context.Context, particion string) ([]rango, error) {
	filas, err := a.pool.Query(ctx, fmt.Sprintf(
		`SELECT cuenta_id, count(*), coalesce(sum(monto_centavos), 0)::bigint FROM %s
		 GROUP BY cuenta_id ORDER BY cuenta_id`, particion))
	if err != nil {
		return nil, fmt.Errorf("contar filas por cuenta: %w", err)
	}
	defer filas.Close()

	var res []rango
	var actual *rango
	for filas.Next() {
		var cuenta, n, suma int64
		if err := filas.Scan(&cuenta, &n, &suma); err != nil {
			return nil, err
		}
		if actual == nil {
			res = append(res, rango{desde: cuenta})
			actual = &res[len(res)-1]
		}
		actual.hasta = cuenta
		actual.filas += n
		actual.suma += suma
		if actual.filas >= a.filasPorParte {
			actual = nil
		}
	}
	return res, filas.Err()
}

func (a *Archivador) exportarYBorrar(ctx context.Context, tareaID int64, res *Resultado) error {
	rangos, err := a.rangos(ctx, res.Particion)
	if err != nil {
		return err
	}

	partes := make([]Parte, len(rangos))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(a.paralelismo)
	for i, r := range rangos {
		g.Go(func() error {
			p, err := a.escribirParte(gctx, res, i, r)
			partes[i] = p
			return err
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}

	var filasEscritas, suma int64
	for _, p := range partes {
		filasEscritas += p.Filas
		suma += p.SumaCentavos
	}
	res.Filas, res.SumaCentavos, res.Partes = filasEscritas, suma, len(partes)

	prefijo := PrefijoDe(res.Periodo)
	res.Clave = prefijo
	res.Objeto = prefijo
	if len(partes) > 0 {
		res.Objeto = strings.TrimSuffix(partes[0].URI, path.Base(partes[0].Clave))
	}

	err = pg.EnTx(ctx, a.pool, func(tx pgx.Tx) error {
		periodo := res.Periodo + "-01"
		if _, err := tx.Exec(ctx, `DELETE FROM archivo_partes WHERE periodo = $1::date`, periodo); err != nil {
			return err
		}
		for _, p := range partes {
			if _, err := tx.Exec(ctx, `
				INSERT INTO archivo_partes
				    (periodo, parte, clave, cuenta_min, cuenta_max, filas, suma_centavos, bytes)
				VALUES ($1::date, $2, $3, $4, $5, $6, $7, $8)`,
				periodo, p.Numero, p.Clave, p.CuentaMin, p.CuentaMax, p.Filas, p.SumaCentavos, p.Bytes); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `
			UPDATE periodos_archivados
			SET clave = $2, uri = $3, filas = $4, suma_centavos = $5, archivado_en = now()
			WHERE periodo = $1::date`,
			periodo, prefijo, res.Objeto, filasEscritas, suma)
		return err
	})
	if err != nil {
		return fmt.Errorf("registrar el periodo archivado: %w", err)
	}

	if _, err := a.pool.Exec(ctx, fmt.Sprintf(`DROP TABLE %s`, res.Particion)); err != nil {
		return fmt.Errorf("borrar particion exportada: %w", err)
	}
	_, err = a.pool.Exec(ctx, `UPDATE tareas SET estado = 'hecha' WHERE id = $1`, tareaID)
	return err
}

func (a *Archivador) escribirParte(ctx context.Context, res *Resultado, numero int, r rango) (Parte, error) {
	p := Parte{Numero: numero, Clave: ClaveParte(res.Periodo, numero), CuentaMin: r.desde, CuentaMax: r.hasta}

	subido, err := a.obj.Subir(ctx, p.Clave, TipoParquet, func(w io.Writer) error {
		filas, err := a.pool.Query(ctx, fmt.Sprintf(`
			SELECT id, orden_id, cuenta_id, contraparte_id, monto_centavos, tramo, creado_en
			FROM %s WHERE cuenta_id BETWEEN $1 AND $2
			ORDER BY cuenta_id, creado_en, id`, res.Particion), r.desde, r.hasta)
		if err != nil {
			return fmt.Errorf("leer particion: %w", err)
		}
		defer filas.Close()

		escritor := nuevoEscritor(w, a.filasPorBloque)
		lote := make([]Fila, 0, 1024)
		for filas.Next() {
			var f Fila
			if err := filas.Scan(&f.ID, &f.OrdenID, &f.CuentaID, &f.ContraparteID,
				&f.MontoCentavos, &f.Tramo, &f.CreadoEn); err != nil {
				return err
			}
			f.CreadoEn = f.CreadoEn.UTC()
			p.Filas++
			p.SumaCentavos += f.MontoCentavos
			if lote = append(lote, f); len(lote) == cap(lote) {
				if _, err := escritor.Write(lote); err != nil {
					return err
				}
				lote = lote[:0]
			}
		}
		if err := filas.Err(); err != nil {
			return err
		}
		if _, err := escritor.Write(lote); err != nil {
			return err
		}
		return escritor.Close()
	})
	if err != nil {
		return p, fmt.Errorf("escribir %s: %w", p.Clave, err)
	}
	p.Bytes, p.URI = subido.Bytes, subido.URI

	if r.filas != p.Filas || r.suma != p.SumaCentavos {
		return p, fmt.Errorf("%s no coincide con la particion: %d/%d filas, %d/%d centavos",
			p.Clave, p.Filas, r.filas, p.SumaCentavos, r.suma)
	}

	enArchivo, err := a.lector.ContarFilas(ctx, p.Clave, p.Bytes)
	if err != nil {
		return p, fmt.Errorf("releer %s: %w", p.Clave, err)
	}
	if enArchivo != p.Filas {
		return p, fmt.Errorf("%s quedo con %d filas y se escribieron %d", p.Clave, enArchivo, p.Filas)
	}
	return p, nil
}
