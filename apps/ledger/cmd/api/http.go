package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"ledger/internal/apierr"
	"ledger/internal/cors"
	"ledger/internal/docs"
	"ledger/internal/ledger"
	"ledger/internal/mock"
	"ledger/internal/sesion"
	"ledger/internal/webhook"
)

type API struct {
	svc  *ledger.Servicio
	pool *pgxpool.Pool
	mock *mock.Servicio
	cors cors.Politica

	sesiones *sesion.Validador

	avisos *webhook.Receptor

	adminClave string

	// Lo que recibe cada cuenta nueva desde la cuenta interna de ajustes. Lo decide el server: si
	// lo mandara el cliente, cualquiera con un token abriría una cuenta con el saldo que quiera.
	bienvenida int64

	secretos Secretos
}

type Secretos struct {
	Mock      string
	Proveedor string
	Webhook   string
	Admin     string
}

const CabeceraAdmin = "X-Admin-Key"

func (a *API) protegido(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.adminClave != "" {

			dada := r.Header.Get(CabeceraAdmin)
			if subtle.ConstantTimeCompare([]byte(dada), []byte(a.adminClave)) != 1 {
				apierr.EscribirError(w, apierr.New(http.StatusUnauthorized, "no_autorizado",
					"falta o no coincide la cabecera %s", CabeceraAdmin))
				return
			}
		}
		h(w, r)
	}
}

func registrar(a *API) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", a.raiz)
	mux.HandleFunc("GET /health", a.salud)

	mux.HandleFunc("GET /sesiones/actual", a.conIdentidad(a.verSesion))

	mux.HandleFunc("POST /cuentas", a.conIdentidad(a.crearCuenta))

	mux.HandleFunc("GET /cuentas", a.conSesion(a.listarCuentas))
	mux.HandleFunc("GET /cuentas/{id}", a.conSesion(a.verCuenta))

	mux.HandleFunc("PATCH /cuentas/{id}", a.conSesion(a.actualizarCuenta))
	mux.HandleFunc("GET /cuentas/{id}/saldo", a.conSesion(a.saldo))
	mux.HandleFunc("GET /cuentas/{id}/movimientos", a.conSesion(a.movimientos))

	mux.HandleFunc("POST /exportaciones", a.conSesion(a.exportar))
	mux.HandleFunc("GET /exportaciones/{id}", a.conSesion(a.verExportacion))

	mux.HandleFunc("POST /transferencias", a.conSesion(a.transferir))
	mux.HandleFunc("GET /transferencias/{id}", a.conSesion(a.verTransferencia))

	mux.HandleFunc("POST /egresos", a.conSesion(a.egresar))
	mux.HandleFunc("GET /egresos/{id}", a.conSesion(a.verEgreso))

	mux.HandleFunc("POST /ingresos", a.conSesion(a.crearIngreso))
	mux.HandleFunc("GET /ingresos/{id}", a.conSesion(a.verIngreso))
	mux.HandleFunc("POST /ingresos/{id}/simular-deposito", a.conSesion(a.simularDeposito))

	mux.HandleFunc("POST /webhooks/recibidos", a.recibirAviso)

	mux.HandleFunc("GET /webhooks/recibidos", a.protegido(a.listarAvisos))

	mux.HandleFunc("POST /admin/cierre", a.protegido(a.cierre))
	mux.HandleFunc("POST /admin/archivado", a.protegido(a.agendarArchivado))
	mux.HandleFunc("POST /admin/semilla", a.protegido(a.semilla))
	mux.HandleFunc("POST /admin/reinicio", a.protegido(a.reinicio))
	mux.HandleFunc("GET /admin/diagnostico", a.protegido(a.diagnostico))
	mux.HandleFunc("GET /admin/proveedor", a.protegido(a.verProveedor))
	mux.HandleFunc("POST /admin/proveedor", a.protegido(a.cambiarProveedor))
	mux.HandleFunc("DELETE /admin/proveedor", a.protegido(a.restaurarProveedor))

	a.mock.Registrar(mux)

	rutaSpec, rutaUI := docs.Paths()
	manejadorSpec, manejadorUI := docs.Handler()
	mux.HandleFunc("GET "+rutaSpec, manejadorSpec)
	mux.HandleFunc("GET "+rutaUI, manejadorUI)

	return a.cors.Envolver(registrarAccesos(mux))
}

func registrarAccesos(siguiente http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inicio := time.Now()
		captura := &respuestaObservada{ResponseWriter: w, estado: http.StatusOK}
		siguiente.ServeHTTP(captura, r)
		slog.Info("request",
			"metodo", r.Method, "ruta", r.URL.Path,
			"estado", captura.estado, "ms", time.Since(inicio).Milliseconds())
	})
}

type respuestaObservada struct {
	http.ResponseWriter
	estado int
}

func (r *respuestaObservada) WriteHeader(estado int) {
	r.estado = estado
	r.ResponseWriter.WriteHeader(estado)
}

func (a *API) salud(w http.ResponseWriter, r *http.Request) {
	ctx, cancelar := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancelar()

	activo := time.Since(arranque).Round(time.Second)
	cuerpo := map[string]any{
		"estado":          "ok",
		"base":            "ok",
		"version":         version,
		"instancia":       instancia,
		"activo":          activo.String(),
		"activo_segundos": int64(activo.Seconds()),
	}

	if err := a.pool.Ping(ctx); err != nil {

		slog.Warn("health: la base no responde", "error", err)
		cuerpo["estado"] = "degradado"
		cuerpo["base"] = "sin_respuesta"
	}
	apierr.Escribir(w, http.StatusOK, cuerpo)
}

func (a *API) crearCuenta(w http.ResponseWriter, r *http.Request) {
	var e ledger.EntradaCuenta
	if !leerJSON(w, r, &e) {
		return
	}
	if s, ok := sesionDe(r); ok {
		if s.CuentaID != 0 {
			apierr.EscribirError(w, apierr.New(http.StatusConflict, "ya_tiene_cuenta",
				"el usuario ya tiene la cuenta %d", s.CuentaID))
			return
		}
		e.Titular = s.Sub
	}
	e.SaldoInicialCentavos = a.bienvenida
	c, err := a.svc.CrearCuenta(r.Context(), e)
	if err != nil {
		apierr.EscribirError(w, err)
		return
	}
	apierr.Escribir(w, http.StatusCreated, conEnlacesAlta(c))
}

func (a *API) saldo(w http.ResponseWriter, r *http.Request) {
	id, ok := idDeRuta(w, r)
	if !ok {
		return
	}
	if !a.exigirTitular(w, r, id) {
		return
	}
	s, err := a.svc.Saldo(r.Context(), id)
	if err != nil {
		apierr.EscribirError(w, err)
		return
	}
	apierr.Escribir(w, http.StatusOK, conEnlacesSaldo(s))
}

func (a *API) movimientos(w http.ResponseWriter, r *http.Request) {
	id, ok := idDeRuta(w, r)
	if !ok {
		return
	}
	if !a.exigirTitular(w, r, id) {
		return
	}

	if v := r.URL.Query().Get("periodo"); v != "" {
		periodo, err := time.Parse("2006-01", v)
		if err != nil {
			apierr.EscribirError(w, apierr.Invalido("periodo tiene que tener formato AAAA-MM"))
			return
		}
		extracto, err := a.svc.MovimientosDePeriodo(r.Context(), id, periodo)
		if err != nil {
			apierr.EscribirError(w, err)
			return
		}
		apierr.Escribir(w, http.StatusOK, conEnlacesExtracto(extracto))
		return
	}

	limite, _ := strconv.Atoi(r.URL.Query().Get("limite"))
	desplazamiento, _ := strconv.Atoi(r.URL.Query().Get("desde"))
	movs, err := a.svc.Movimientos(r.Context(), id, limite, desplazamiento)
	if err != nil {
		apierr.EscribirError(w, err)
		return
	}
	apierr.Escribir(w, http.StatusOK, map[string]any{"movimientos": movs})
}

func responderOrden(w http.ResponseWriter, o *ledger.Orden, err error) {
	if err != nil {
		apierr.EscribirError(w, err)
		return
	}
	apierr.Escribir(w, ledger.EstadoHTTP(o), conEnlacesOrden(o))
}

func responderOrdenNueva(w http.ResponseWriter, o *ledger.Orden, err error) {
	if err != nil {
		apierr.EscribirError(w, err)
		return
	}
	apierr.Escribir(w, http.StatusCreated, conEnlacesOrden(o))
}

func (a *API) transferir(w http.ResponseWriter, r *http.Request) {
	clave, ok := claveIdempotencia(w, r)
	if !ok {
		return
	}
	var e ledger.EntradaTransferencia
	if !leerJSON(w, r, &e) {
		return
	}
	e.IdempotencyKey = clave
	if !a.exigirTitular(w, r, e.OrigenID) {
		return
	}

	o, err := a.svc.Transferir(r.Context(), e)
	responderOrden(w, o, err)
}

func (a *API) verTransferencia(w http.ResponseWriter, r *http.Request) {
	id, ok := idDeRuta(w, r)
	if !ok {
		return
	}
	o, err := a.svc.Consultar(r.Context(), ledger.TipoTransferencia, id)
	if err == nil && !a.exigirParteDeLaOrden(w, r, o) {
		return
	}
	responderOrden(w, o, err)
}

func (a *API) egresar(w http.ResponseWriter, r *http.Request) {
	clave, ok := claveIdempotencia(w, r)
	if !ok {
		return
	}
	var e ledger.EntradaEgreso
	if !leerJSON(w, r, &e) {
		return
	}
	e.IdempotencyKey = clave
	if !a.exigirTitular(w, r, e.OrigenID) {
		return
	}

	o, err := a.svc.Egresar(r.Context(), e)
	responderOrden(w, o, err)
}

func (a *API) verEgreso(w http.ResponseWriter, r *http.Request) {
	id, ok := idDeRuta(w, r)
	if !ok {
		return
	}

	o, err := a.svc.ConsultarEgreso(r.Context(), id)
	if err == nil && !a.exigirParteDeLaOrden(w, r, o) {
		return
	}
	responderOrden(w, o, err)
}

func (a *API) crearIngreso(w http.ResponseWriter, r *http.Request) {
	clave, ok := claveIdempotencia(w, r)
	if !ok {
		return
	}
	var e ledger.EntradaIngreso
	if !leerJSON(w, r, &e) {
		return
	}
	e.IdempotencyKey = clave
	if !a.exigirTitular(w, r, e.DestinoID) {
		return
	}

	o, err := a.svc.CrearIntencionIngreso(r.Context(), e)
	responderOrdenNueva(w, o, err)
}

func (a *API) simularDeposito(w http.ResponseWriter, r *http.Request) {
	id, ok := idDeRuta(w, r)
	if !ok {
		return
	}

	o, err := a.svc.Consultar(r.Context(), ledger.TipoIngreso, id)
	if err != nil {
		apierr.EscribirError(w, err)
		return
	}
	if !a.exigirParteDeLaOrden(w, r, o) {
		return
	}

	actualizada, err := a.svc.SimularDeposito(r.Context(), id)
	responderOrden(w, actualizada, err)
}

func (a *API) verIngreso(w http.ResponseWriter, r *http.Request) {
	id, ok := idDeRuta(w, r)
	if !ok {
		return
	}

	o, err := a.svc.ConsultarIngreso(r.Context(), id)
	if err == nil && !a.exigirParteDeLaOrden(w, r, o) {
		return
	}
	responderOrden(w, o, err)
}

func (a *API) cierre(w http.ResponseWriter, r *http.Request) {
	c, err := a.svc.VerificarCierre(r.Context())
	if err != nil {
		apierr.EscribirError(w, err)
		return
	}
	apierr.Escribir(w, http.StatusOK, c)
}

func (a *API) agendarArchivado(w http.ResponseWriter, r *http.Request) {
	var periodo *string
	err := a.pool.QueryRow(r.Context(), `
		WITH elegible AS (
			SELECT min(periodo) AS p FROM asientos
			WHERE periodo < date_trunc('month', now())::date - make_interval(months => $1::int)
		)
		INSERT INTO tareas (tipo, periodo, proxima_ejecucion)
		SELECT 'archivado', p, now() FROM elegible WHERE p IS NOT NULL
		ON CONFLICT (tipo, periodo)
		DO UPDATE SET proxima_ejecucion = now(), estado = 'pendiente'
		RETURNING to_char(periodo, 'YYYY-MM')`, retencionMeses(r)).Scan(&periodo)
	if err != nil {
		if err.Error() == "no rows in result set" {
			apierr.Escribir(w, http.StatusOK, map[string]any{
				"agendado": false,
				"motivo":   "no hay ningun periodo cerrado fuera de la ventana de retencion",
			})
			return
		}
		apierr.EscribirError(w, err)
		return
	}
	apierr.Escribir(w, http.StatusAccepted, map[string]any{"agendado": true, "periodo": periodo})
}

func retencionMeses(r *http.Request) int {
	if v, err := strconv.Atoi(r.URL.Query().Get("retencion_meses")); err == nil && v >= 0 {
		return v
	}
	return 12
}

func (a *API) semilla(w http.ResponseWriter, r *http.Request) {
	var e ledger.EntradaSemilla
	if !leerJSON(w, r, &e) {
		return
	}
	n, err := a.svc.SembrarPeriodo(r.Context(), e)
	if err != nil {
		apierr.EscribirError(w, err)
		return
	}
	apierr.Escribir(w, http.StatusCreated, map[string]any{"transferencias": n, "periodo": e.Periodo})
}

func (a *API) reinicio(w http.ResponseWriter, r *http.Request) {
	var e struct {
		Confirmar string `json:"confirmar"`
	}
	if !leerJSON(w, r, &e) {
		return
	}
	if e.Confirmar != ledger.ConfirmacionReinicio {
		apierr.EscribirError(w, apierr.Invalido(
			"para borrar los datos hay que mandar {\"confirmar\": %q}. Se borran todas las "+
				"cuentas, ordenes y asientos; el esquema y las cuatro cuentas internas quedan",
			ledger.ConfirmacionReinicio))
		return
	}

	res, err := a.svc.ReiniciarDatos(r.Context())
	if err != nil {
		apierr.EscribirError(w, err)
		return
	}
	slog.Warn("reinicio de datos ejecutado", "desde", r.RemoteAddr)
	apierr.Escribir(w, http.StatusOK, res)
}

func claveIdempotencia(w http.ResponseWriter, r *http.Request) (string, bool) {
	clave := r.Header.Get("Idempotency-Key")
	if clave == "" {
		apierr.EscribirError(w, apierr.Invalido("falta la cabecera Idempotency-Key"))
		return "", false
	}

	if strings.Contains(clave, ":") {
		apierr.EscribirError(w, apierr.Invalido("la Idempotency-Key no puede contener ':'"))
		return "", false
	}
	return clave, true
}

func idDeRuta(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		apierr.EscribirError(w, apierr.Invalido("el id tiene que ser un numero"))
		return 0, false
	}
	return id, true
}

func leerJSON(w http.ResponseWriter, r *http.Request, destino any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(destino); err != nil {
		apierr.EscribirError(w, apierr.Invalido("cuerpo invalido: %v", err))
		return false
	}
	return true
}

func (a *API) listarCuentas(w http.ResponseWriter, r *http.Request) {
	limite, _ := strconv.Atoi(r.URL.Query().Get("limite"))
	desde, _ := strconv.Atoi(r.URL.Query().Get("desde"))
	cuentas, err := a.svc.ListarCuentas(r.Context(), r.URL.Query().Get("tipo"), limite, desde)
	if err != nil {
		apierr.EscribirError(w, err)
		return
	}

	if mia := titular(r); mia != 0 {
		vista := make([]any, 0, len(cuentas))
		for i := range cuentas {
			if cuentas[i].ID == mia {
				vista = append(vista, cuentas[i])
				continue
			}
			vista = append(vista, cuentas[i].Publica())
		}
		apierr.Escribir(w, http.StatusOK, map[string]any{"cuentas": vista})
		return
	}
	apierr.Escribir(w, http.StatusOK, map[string]any{"cuentas": cuentas})
}

func (a *API) verCuenta(w http.ResponseWriter, r *http.Request) {
	id, ok := idDeRuta(w, r)
	if !ok {
		return
	}
	c, err := a.svc.VerCuenta(r.Context(), id)
	if err != nil {
		apierr.EscribirError(w, err)
		return
	}

	if mia := titular(r); mia != 0 && mia != c.ID {
		apierr.Escribir(w, http.StatusOK, c.Publica())
		return
	}
	apierr.Escribir(w, http.StatusOK, conEnlacesCuenta(c))
}

func (a *API) actualizarCuenta(w http.ResponseWriter, r *http.Request) {
	id, ok := idDeRuta(w, r)
	if !ok {
		return
	}
	if !a.exigirTitular(w, r, id) {
		return
	}
	var c ledger.CambioCuenta
	if !leerJSON(w, r, &c) {
		return
	}
	actualizada, err := a.svc.ActualizarCuenta(r.Context(), id, c)
	if err != nil {
		apierr.EscribirError(w, err)
		return
	}
	apierr.Escribir(w, http.StatusOK, conEnlacesCuenta(actualizada))
}

func (a *API) exportar(w http.ResponseWriter, r *http.Request) {
	var e ledger.EntradaExportacion
	if !leerJSON(w, r, &e) {
		return
	}

	if mia := titular(r); mia != 0 {
		if e.CuentaID == 0 {
			e.CuentaID = mia
		} else if e.CuentaID != mia {
			apierr.EscribirError(w, apierr.New(http.StatusForbidden, "cuenta_ajena",
				"la sesion es de la cuenta %d y la exportacion pide la %d", mia, e.CuentaID))
			return
		}
	}
	res, err := a.svc.Exportar(r.Context(), e)
	if err != nil {
		apierr.EscribirError(w, err)
		return
	}
	apierr.Escribir(w, http.StatusCreated, conEnlacesExportacion(res))
}

func (a *API) verExportacion(w http.ResponseWriter, r *http.Request) {
	id, ok := idDeRuta(w, r)
	if !ok {
		return
	}
	res, err := a.svc.VerExportacion(r.Context(), id)
	if err != nil {
		apierr.EscribirError(w, err)
		return
	}
	if mia := titular(r); mia != 0 && (res.CuentaID == nil || *res.CuentaID != mia) {
		apierr.EscribirError(w, apierr.New(http.StatusForbidden, "exportacion_ajena",
			"la exportacion %d no es de la cuenta %d", res.ID, mia))
		return
	}
	apierr.Escribir(w, http.StatusOK, conEnlacesExportacion(res))
}

func (a *API) recibirAviso(w http.ResponseWriter, r *http.Request) {
	cuerpo, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		apierr.EscribirError(w, apierr.Invalido("no se pudo leer el aviso: %v", err))
		return
	}

	guardado, err := a.avisos.Recibir(r.Context(), cuerpo, r.Header.Get(webhook.CabeceraFirma), r.RemoteAddr)
	if err != nil {
		if guardado != nil {
			slog.Warn("aviso recibido con firma invalida", "orden", guardado.OrdenID, "desde", r.RemoteAddr)
		}
		apierr.EscribirError(w, err)
		return
	}
	slog.Info("aviso recibido", "orden", guardado.OrdenID, "estado", guardado.Estado, "desde", r.RemoteAddr)
	apierr.Escribir(w, http.StatusOK, guardado)
}

func (a *API) listarAvisos(w http.ResponseWriter, r *http.Request) {
	limite, _ := strconv.Atoi(r.URL.Query().Get("limite"))
	avisos, err := a.avisos.Listar(r.Context(), limite)
	if err != nil {
		apierr.EscribirError(w, err)
		return
	}
	apierr.Escribir(w, http.StatusOK, map[string]any{"avisos": avisos})
}

func (a *API) raiz(w http.ResponseWriter, _ *http.Request) {
	apierr.Escribir(w, http.StatusOK, documentoRaiz())
}

func (a *API) verProveedor(w http.ResponseWriter, r *http.Request) {
	p, err := a.svc.VerProveedor(r.Context())
	if err != nil {
		apierr.EscribirError(w, err)
		return
	}
	apierr.Escribir(w, http.StatusOK, p)
}

func (a *API) cambiarProveedor(w http.ResponseWriter, r *http.Request) {
	var e struct {
		URL string `json:"url"`
	}
	if !leerJSON(w, r, &e) {
		return
	}
	p, err := a.svc.CambiarProveedor(r.Context(), e.URL, r.RemoteAddr)
	if err != nil {
		apierr.EscribirError(w, err)
		return
	}
	slog.Info("proveedor cambiado", "url", p.URL, "desde", r.RemoteAddr)
	apierr.Escribir(w, http.StatusOK, p)
}

func (a *API) restaurarProveedor(w http.ResponseWriter, r *http.Request) {
	p, err := a.svc.RestaurarProveedor(r.Context())
	if err != nil {
		apierr.EscribirError(w, err)
		return
	}
	slog.Info("proveedor restaurado al valor de PROVEEDOR_URL", "url", p.URL)
	apierr.Escribir(w, http.StatusOK, p)
}
