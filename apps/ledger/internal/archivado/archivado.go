package archivado

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"ledger/internal/objetos"
	"ledger/internal/pg"
)

const estadosTerminales = `'liquidada','rechazada','acreditada','devuelta','atribuida','no_atribuida'`

const tipoTarea = "archivado"

const prefijoArchivo = "transaction-history/"

func ClaveDe(periodo string) string {
	return fmt.Sprintf("%speriodo=%s/asientos.ndjson.gz", prefijoArchivo, periodo)
}

type Archivador struct {
	pool           *pgxpool.Pool
	obj            objetos.Almacen
	retencionMeses int
}

func Nuevo(pool *pgxpool.Pool, obj objetos.Almacen, retencionMeses int) *Archivador {
	return &Archivador{pool: pool, obj: obj, retencionMeses: retencionMeses}
}

type Resultado struct {
	Periodo      string `json:"periodo"`
	Particion    string `json:"particion,omitempty"`
	Filas        int64  `json:"filas"`
	SumaCentavos int64  `json:"suma_centavos"`
	Objeto       string `json:"objeto,omitempty"`
	Clave        string `json:"clave,omitempty"`
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

type Asiento struct {
	ID            int64     `json:"id"`
	Periodo       string    `json:"periodo"`
	OrdenID       int64     `json:"orden_id"`
	CuentaID      int64     `json:"cuenta_id"`
	ContraparteID int64     `json:"contraparte_id"`
	MontoCentavos int64     `json:"monto_centavos"`
	Tramo         string    `json:"tramo"`
	CreadoEn      time.Time `json:"creado_en"`
}

func (a *Archivador) exportarYBorrar(ctx context.Context, tareaID int64, res *Resultado) error {
	filas, err := a.pool.Query(ctx, fmt.Sprintf(`
		SELECT id, periodo, orden_id, cuenta_id, contraparte_id, monto_centavos, tramo, creado_en
		FROM %s ORDER BY cuenta_id, creado_en, id`, res.Particion))
	if err != nil {
		return fmt.Errorf("leer particion: %w", err)
	}

	var cuerpo bytes.Buffer
	comprimido := gzip.NewWriter(&cuerpo)
	codificador := json.NewEncoder(comprimido)
	var filasLeidas, suma int64
	for filas.Next() {
		var e Asiento
		var periodo time.Time
		if err := filas.Scan(&e.ID, &periodo, &e.OrdenID, &e.CuentaID, &e.ContraparteID,
			&e.MontoCentavos, &e.Tramo, &e.CreadoEn); err != nil {
			filas.Close()
			return err
		}
		e.Periodo = periodo.Format("2006-01-02")
		if err := codificador.Encode(e); err != nil {
			filas.Close()
			return err
		}
		filasLeidas++
		suma += e.MontoCentavos
	}
	filas.Close()
	if err := filas.Err(); err != nil {
		return err
	}
	res.Filas, res.SumaCentavos = filasLeidas, suma

	var filasControl, sumaControl int64
	if err := a.pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT count(*), coalesce(sum(monto_centavos), 0) FROM %s`, res.Particion),
	).Scan(&filasControl, &sumaControl); err != nil {
		return err
	}
	if filasControl != filasLeidas || sumaControl != suma {
		return fmt.Errorf("la exportacion no coincide con la particion: %d/%d filas, %d/%d centavos",
			filasLeidas, filasControl, suma, sumaControl)
	}

	if err := comprimido.Close(); err != nil {
		return err
	}

	clave := ClaveDe(res.Periodo)
	uri, err := a.obj.Guardar(ctx, clave, cuerpo.Bytes(), "application/gzip")
	if err != nil {
		return err
	}
	res.Objeto = uri
	res.Clave = clave

	if _, err := a.pool.Exec(ctx, `
		UPDATE periodos_archivados
		SET clave = $2, uri = $3, filas = $4, suma_centavos = $5, archivado_en = now()
		WHERE periodo = $1::date`,
		res.Periodo+"-01", clave, uri, filasLeidas, suma); err != nil {
		return fmt.Errorf("registrar el periodo archivado: %w", err)
	}

	if _, err := a.pool.Exec(ctx, fmt.Sprintf(`DROP TABLE %s`, res.Particion)); err != nil {
		return fmt.Errorf("borrar particion exportada: %w", err)
	}
	_, err = a.pool.Exec(ctx, `UPDATE tareas SET estado = 'hecha' WHERE id = $1`, tareaID)
	return err
}
