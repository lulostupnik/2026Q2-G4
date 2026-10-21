// El sitio se sirve desde el bucket y la API está detrás del NLB: todo pedido es cross-origin.
// Lo que protege la API no es el origen sino el access token que viaja en Authorization.

const LLAVES = {
  sesion: 'nucleo.sesion',
  admin: 'nucleo.admin',
  mock: 'nucleo.mock',
};

// sessionStorage y no localStorage: el token y las claves de operación viven lo que vive la
// pestaña, no quedan en el disco para el próximo que use la computadora. Si Cognito todavía
// tiene la sesión abierta, volver a entrar es un clic. Puede tirar en ventanas privadas o con
// las cookies bloqueadas: mejor no recordar la sesión que no arrancar.
function leer(llave) {
  try { return sessionStorage.getItem(llave) || ''; } catch { return ''; }
}
function guardar(llave, valor) {
  try { valor ? sessionStorage.setItem(llave, valor) : sessionStorage.removeItem(llave); } catch { /* sin persistencia */ }
}

// Versiones anteriores guardaban todo en localStorage: se limpia lo que haya quedado.
try { Object.values(LLAVES).forEach((llave) => localStorage.removeItem(llave)); } catch { /* sin persistencia */ }

function leerSesion() {
  try {
    const s = JSON.parse(leer(LLAVES.sesion) || 'null');
    if (s?.token && s.expira > Date.now()) return s;
  } catch { /* sesión ilegible: se descarta */ }
  guardar(LLAVES.sesion, '');
  return null;
}

const guardada = leerSesion();

export const estado = {
  base: '',
  token: guardada?.token || '',
  idToken: guardada?.idToken || '',
  expira: guardada?.expira || 0,
  cuenta: null,
  admin: leer(LLAVES.admin),
  mock: leer(LLAVES.mock),
};

export function fijarBase(base) {
  estado.base = String(base || '').trim().replace(/\/+$/, '');
}

function reclamos(jwt) {
  try {
    const b64 = jwt.split('.')[1].replace(/-/g, '+').replace(/_/g, '/');
    return JSON.parse(new TextDecoder().decode(Uint8Array.from(atob(b64), (c) => c.charCodeAt(0))));
  } catch { return {}; }
}

export function fijarTokens({ accessToken, idToken, expiraEnSegundos }) {
  const segundos = Number(expiraEnSegundos);
  const porDuracion = segundos > 0 ? Date.now() + segundos * 1000 : Infinity;
  const porReclamo = Number(reclamos(accessToken).exp) * 1000 || Infinity;
  let expira = Math.min(porDuracion, porReclamo);
  if (!Number.isFinite(expira)) expira = Date.now() + 3600 * 1000;

  estado.token = accessToken || '';
  estado.idToken = idToken || '';
  estado.expira = expira;
  estado.cuenta = null;
  guardar(LLAVES.sesion, JSON.stringify({ token: estado.token, idToken: estado.idToken, expira }));
}

export function emailDeLaSesion() {
  return reclamos(estado.idToken).email || '';
}

export function fijarCuenta(cuenta) {
  estado.cuenta = cuenta || null;
}

// Las claves de operación se van con la sesión: el que entre después en esta pestaña no hereda
// el acceso de administrador.
export function cerrarSesion() {
  estado.token = '';
  estado.idToken = '';
  estado.expira = 0;
  estado.cuenta = null;
  guardar(LLAVES.sesion, '');
  fijarClaves({ admin: '', mock: '' });
}

export function fijarClaves({ admin, mock }) {
  if (admin !== undefined) { estado.admin = admin; guardar(LLAVES.admin, admin); }
  if (mock !== undefined) { estado.mock = mock; guardar(LLAVES.mock, mock); }
}

export function modoOperacion() { return Boolean(estado.admin); }

export function hayToken() { return Boolean(estado.token) && estado.expira > Date.now(); }
export function haySesion() { return hayToken() && Boolean(estado.cuenta); }

const cinta = [];
const oyentes = new Set();
const MAXIMO_CINTA = 200;

export function alAnotar(fn) { oyentes.add(fn); return () => oyentes.delete(fn); }
export function verCinta() { return cinta; }
export function limpiarCinta() { cinta.length = 0; oyentes.forEach((fn) => fn(null)); }

function anotar(entrada) {
  cinta.unshift(entrada);
  if (cinta.length > MAXIMO_CINTA) cinta.length = MAXIMO_CINTA;
  oyentes.forEach((fn) => fn(entrada));
}

const caidas = new Set();
export function alCaerSesion(fn) { caidas.add(fn); return () => caidas.delete(fn); }

export function vigente() {
  if (hayToken()) return true;
  if (estado.token) {
    cerrarSesion();
    caidas.forEach((fn) => fn('sesion_vencida'));
  }
  return false;
}

const sinCuenta = new Set();
export function alPerderCuenta(fn) { sinCuenta.add(fn); return () => sinCuenta.delete(fn); }

let contador = 0;

// Nunca lanza por un código HTTP: un 422 es el "no" del negocio y un 202 una operación en duda,
// y la pantalla tiene que saber mostrar los dos. Sólo lanza si no hubo respuesta.
export async function pedir(metodo, ruta, opciones = {}) {
  const { cuerpo, clave, admin, mock, sinSesion } = opciones;
  const cabeceras = {};

  if (admin && !estado.admin) return sinClave(metodo, ruta, 'admin');
  if (mock && !estado.mock) return sinClave(metodo, ruta, 'mock');

  // Un 401 de /admin o de /servicio-mock habla de su cabecera, no de la sesión: no tiene que
  // sacar al usuario.
  const porLaSesion = !admin && !mock && !sinSesion;
  if (porLaSesion && estado.token && !vigente()) {
    const datos = { codigo: 'sesion_vencida', mensaje: 'Tu sesión venció: volvé a ingresar' };
    anotar({ id: ++contador, metodo, ruta, codigo: 0, ms: 0, datos });
    return { ok: false, codigo: 0, datos };
  }

  if (cuerpo !== undefined) cabeceras['Content-Type'] = 'application/json';
  if (clave) cabeceras['Idempotency-Key'] = clave;
  if (admin) cabeceras['X-Admin-Key'] = estado.admin;
  if (mock) cabeceras['X-Mock-Key'] = estado.mock;
  if (estado.token && !sinSesion) cabeceras.Authorization = `Bearer ${estado.token}`;

  const arranque = performance.now();
  const id = ++contador;

  let respuesta;
  try {
    respuesta = await fetch(estado.base + ruta, {
      method: metodo,
      headers: cabeceras,
      body: cuerpo === undefined ? undefined : JSON.stringify(cuerpo),
    });
  } catch (err) {
    anotar({ id, metodo, ruta, codigo: 0, ms: Math.round(performance.now() - arranque), error: String(err) });
    throw new Error(`no se pudo contactar la API en ${estado.base || '(sin configurar)'}`);
  }

  const ms = Math.round(performance.now() - arranque);
  const texto = await respuesta.text();
  let datos = null;
  try { datos = texto ? JSON.parse(texto) : null; } catch { datos = { crudo: texto }; }

  anotar({ id, metodo, ruta, codigo: respuesta.status, ms, datos });

  if (respuesta.status === 401 && estado.token && porLaSesion) {
    cerrarSesion();
    caidas.forEach((fn) => fn(datos?.codigo || 'sesion_invalida'));
  } else if (respuesta.status === 403 && datos?.codigo === 'sin_cuenta' && porLaSesion) {
    estado.cuenta = null;
    sinCuenta.forEach((fn) => fn());
  }

  return { ok: respuesta.ok, codigo: respuesta.status, datos };
}

function sinClave(metodo, ruta, cual) {
  const datos = {
    codigo: 'sin_clave',
    mensaje: `Falta la clave de ${cual}: cargala en «Modo operación»`,
  };
  anotar({ id: ++contador, metodo, ruta, codigo: 0, ms: 0, datos });
  return { ok: false, codigo: 0, datos };
}

export async function consultarSalud() {
  const r = await pedir('GET', '/health', { sinSesion: true });
  if (!r.ok || typeof r.datos?.estado !== 'string') {
    throw new Error(`La API no está sana: /health respondió ${r.codigo}`);
  }
  return r.datos;
}

export async function exigir(metodo, ruta, opciones = {}) {
  const r = await pedir(metodo, ruta, opciones);
  if (!r.ok) throw new Error(mensajeDeError(r));
  return r.datos;
}

export function mensajeDeError(r) {
  const d = r?.datos;
  if (!d) return `la API respondio ${r?.codigo}`;
  return d.mensaje || d.detalle || d.error || d.message || `la API respondio ${r.codigo}`;
}

// El sitio se sirve por HTTP desde S3, y crypto.randomUUID no existe fuera de un contexto seguro.
export function aleatorio(bytes = 16) {
  return Array.from(crypto.getRandomValues(new Uint8Array(bytes)), (b) => b.toString(16).padStart(2, '0')).join('');
}

export function nuevaClave(prefijo = 'app') {
  return `${prefijo}-${aleatorio()}`;
}

// La misma Idempotency-Key mientras se reenvíen los mismos datos y una nueva en cuanto cambian:
// si la red se corta después de que el server liquidó, el reintento no debita dos veces.
export function claveDeIntento(prefijo) {
  let firma = null;
  let clave = null;
  return (cuerpo) => {
    const actual = JSON.stringify(cuerpo);
    if (actual !== firma) {
      firma = actual;
      clave = nuevaClave(prefijo);
    }
    return clave;
  };
}
