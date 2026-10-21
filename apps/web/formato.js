import { icono } from './iconos.js';

// Con textContent un alias que contenga "<" se ve como texto y no se ejecuta: por eso el()
// no acepta html.
export function el(etiqueta, props = {}, ...hijos) {
  const nodo = document.createElement(etiqueta);
  for (const [k, v] of Object.entries(props)) {
    if (v === undefined || v === null || v === false) continue;
    if (k === 'clase') nodo.className = v;
    else if (k === 'texto') nodo.textContent = v;
    else if (k === 'html') throw new Error('no se asigna html: usá texto');
    else if (k.startsWith('on')) nodo.addEventListener(k.slice(2).toLowerCase(), v);
    else if (k === 'dataset') Object.assign(nodo.dataset, v);
    else if (k in nodo && k !== 'list') nodo[k] = v;
    else nodo.setAttribute(k, v);
  }
  for (const h of hijos.flat()) {
    if (h === null || h === undefined || h === false) continue;
    nodo.append(h.nodeType ? h : document.createTextNode(String(h)));
  }
  return nodo;
}

export function vaciar(nodo) { while (nodo.firstChild) nodo.removeChild(nodo.firstChild); }

const pesos = new Intl.NumberFormat('es-AR', {
  style: 'currency', currency: 'ARS', minimumFractionDigits: 2, maximumFractionDigits: 2,
});

export function enPesos(centavos) {
  return pesos.format(Number(centavos ?? 0) / 100);
}

export function conSigno(centavos) {
  const n = Number(centavos ?? 0);
  return (n > 0 ? '+ ' : n < 0 ? '- ' : '') + pesos.format(Math.abs(n) / 100);
}

// El importe se parte como texto y nunca pasa por punto flotante: parseFloat("0.1") * 100 da
// 10.000000000000002. Si hay coma, es el decimal y el punto es de miles; si sólo hay punto,
// es decimal cuando lo siguen una o dos cifras.
export function aCentavos(entrada) {
  const texto = String(entrada ?? '').trim().replace(/\s/g, '');
  if (!texto) return null;

  let normal = texto;
  if (texto.includes(',')) {
    normal = texto.replace(/\./g, '').replace(',', '.');
  } else {
    const punto = texto.lastIndexOf('.');
    const decimales = punto === -1 ? -1 : texto.length - punto - 1;
    if (punto !== -1 && decimales > 2) normal = texto.replace(/\./g, '');
  }

  if (!/^\d+(\.\d{1,2})?$/.test(normal)) return null;

  const [enteros, fraccion = ''] = normal.split('.');
  return Number(enteros) * 100 + Number(fraccion.padEnd(2, '0'));
}

const horaCorta = new Intl.DateTimeFormat('es-AR', { hour: '2-digit', minute: '2-digit' });
const diaCorto = new Intl.DateTimeFormat('es-AR', { day: 'numeric', month: 'short' });
const completa = new Intl.DateTimeFormat('es-AR', { dateStyle: 'medium', timeStyle: 'medium' });
const mesLargo = new Intl.DateTimeFormat('es-AR', { month: 'long', year: 'numeric', timeZone: 'UTC' });

export function cuando(iso) {
  if (!iso) return '';
  const f = new Date(iso);
  if (Number.isNaN(f.getTime())) return '';
  const hoy = new Date();
  const mismoDia = f.toDateString() === hoy.toDateString();
  return mismoDia ? horaCorta.format(f) : diaCorto.format(f);
}

export function hora(iso) {
  const f = new Date(iso);
  return Number.isNaN(f.getTime()) ? '' : horaCorta.format(f);
}

export function fechaCompleta(iso) {
  if (!iso) return '';
  const f = new Date(iso);
  return Number.isNaN(f.getTime()) ? '' : completa.format(f);
}

// Los períodos del ledger son meses en UTC, no en la hora local del navegador.
export function periodoActual(desplazamiento = 0) {
  const hoy = new Date();
  const mes = new Date(Date.UTC(hoy.getUTCFullYear(), hoy.getUTCMonth() + desplazamiento, 1));
  return `${mes.getUTCFullYear()}-${String(mes.getUTCMonth() + 1).padStart(2, '0')}`;
}

export function nombreDelPeriodo(periodo) {
  const f = new Date(`${periodo}-01T00:00:00Z`);
  if (Number.isNaN(f.getTime())) return periodo;
  const texto = mesLargo.format(f);
  return texto.charAt(0).toUpperCase() + texto.slice(1);
}

export function avisar(texto, malo = false) {
  const caja = document.getElementById('avisos');
  const nodo = el('div', { clase: `aviso${malo ? ' aviso-malo' : ''}`, role: malo ? 'alert' : 'status' },
    icono(malo ? 'alerta' : 'ok', 18), el('span', { texto }));
  caja.append(nodo);
  setTimeout(() => nodo.classList.add('aviso-sale'), 3400);
  setTimeout(() => nodo.remove(), 3800);
}

// La API tiene nueve estados porque el ledger los necesita; a una persona le alcanzan tres
// desenlaces: salió, no salió o todavía no se sabe. El panel de operación muestra el crudo.
const DESENLACES = {
  liquidada:    { texto: 'Enviado',      tono: 'ok' },
  acreditada:   { texto: 'Enviado',      tono: 'ok' },
  atribuida:    { texto: 'Acreditado',   tono: 'ok' },
  rechazada:    { texto: 'Rechazado',    tono: 'no' },
  devuelta:     { texto: 'Devuelto',     tono: 'no' },
  no_atribuida: { texto: 'Sin acreditar', tono: 'no' },
  en_duda:      { texto: 'En revisión',  tono: 'espera' },
  reservada:    { texto: 'En curso',     tono: 'espera' },
  enviada:      { texto: 'En curso',     tono: 'espera' },
  pendiente:    { texto: 'Pendiente',    tono: 'espera' },
};

export function desenlace(estado) {
  return DESENLACES[estado] || { texto: estado || '—', tono: '' };
}

export function iniciales(alias) {
  return String(alias || '?').replace(/[^\p{L}\p{N}]+/gu, ' ').trim().split(' ')
    .slice(0, 2).map((p) => p[0] || '').join('') || '?';
}

// El mismo alias siempre da el mismo color: la persona se reconoce en la lista sin leer.
export function colorDe(texto) {
  let h = 0;
  for (const c of String(texto || '')) h = (h * 31 + c.codePointAt(0)) % 360;
  return h;
}

export function avatar(alias, tam = 'normal') {
  return el('span', {
    clase: `avatar avatar-${tam}`,
    style: `--tono:${colorDe(alias)}`,
    texto: iniciales(alias).toUpperCase(),
    'aria-hidden': 'true',
  });
}

const diaLargo = new Intl.DateTimeFormat('es-AR', { weekday: 'long', day: 'numeric', month: 'long' });

// Para agrupar la actividad: "Hoy", "Ayer" o "martes 14 de octubre".
export function etiquetaDia(iso) {
  const f = new Date(iso);
  if (Number.isNaN(f.getTime())) return '';
  const dia = (d) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
  const diferencia = Math.round((dia(new Date()) - dia(f)) / 86400000);
  if (diferencia === 0) return 'Hoy';
  if (diferencia === 1) return 'Ayer';
  const texto = diaLargo.format(f);
  return texto.charAt(0).toUpperCase() + texto.slice(1);
}

// El sitio se sirve por HTTP y navigator.clipboard sólo existe en contextos seguros: si no está,
// se copia con un textarea temporal, que anda en cualquier página.
export async function copiar(texto) {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(texto);
      return true;
    }
  } catch { /* sigue con el respaldo */ }
  const area = el('textarea', { value: texto, readOnly: true, style: 'position:fixed;opacity:0' });
  document.body.append(area);
  area.select();
  let ok = false;
  try { ok = document.execCommand('copy'); } catch { ok = false; }
  area.remove();
  return ok;
}

export function botonCopiar(texto, rotulo = 'Copiar') {
  return el('button', {
    type: 'button', clase: 'boton-icono', title: rotulo, 'aria-label': rotulo,
    onclick: async () => avisar((await copiar(texto)) ? 'Copiado' : 'No se pudo copiar', false),
  }, icono('copiar', 16));
}

// Mientras llega la respuesta: barras grises con la forma de lo que va a aparecer.
export function esqueleto(filas = 3) {
  return el('div', { clase: 'esqueleto', 'aria-busy': 'true', 'aria-label': 'Cargando' },
    ...Array.from({ length: filas }, () => el('div', { clase: 'esqueleto-fila' },
      el('span', { clase: 'esqueleto-circulo' }),
      el('span', { clase: 'esqueleto-lineas' }, el('span'), el('span')))));
}

export function vacio(nombreIcono, titulo, detalle = '', ...acciones) {
  return el('div', { clase: 'vacio' },
    el('span', { clase: 'vacio-icono' }, icono(nombreIcono, 26)),
    el('p', { clase: 'vacio-titulo', texto: titulo }),
    detalle ? el('p', { clase: 'vacio-detalle', texto: detalle }) : null,
    acciones.length ? el('div', { clase: 'fila-botones' }, ...acciones) : null);
}
