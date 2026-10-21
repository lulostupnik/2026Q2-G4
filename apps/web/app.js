import {
  estado, fijarBase, fijarTokens, fijarCuenta, fijarClaves, cerrarSesion, hayToken, haySesion,
  emailDeLaSesion, pedir, consultarSalud, vigente, alCaerSesion, alPerderCuenta, mensajeDeError,
  modoOperacion,
} from './api.js';
import { leerConfig, irAlLogin, irAlLogout, leerVuelta } from './cognito.js';
import { el, vaciar, avisar, avatar } from './formato.js';
import { icono } from './iconos.js';
import * as usuario from './usuario.js';
import { panel } from './panel.js';

const acceso = document.getElementById('acceso');
const aplicacion = document.getElementById('aplicacion');
const hoja = document.getElementById('hoja');
const nav = document.getElementById('nav');
const navMovil = document.getElementById('nav-movil');
const lateralPie = document.getElementById('lateral-pie');

const SECCIONES = [
  { hash: '#/inicio', texto: 'Inicio', icono: 'inicio', pintar: usuario.inicio },
  { hash: '#/enviar', texto: 'Enviar', icono: 'enviar', pintar: usuario.enviar },
  { hash: '#/ingresar', texto: 'Ingresar', icono: 'ingresar', pintar: usuario.ingresar },
  { hash: '#/retirar', texto: 'Retirar', icono: 'retirar', pintar: usuario.retirar },
  { hash: '#/actividad', texto: 'Actividad', icono: 'actividad', pintar: usuario.actividad },
  // Sólo aparece con la clave de admin cargada: un usuario común no tiene nada que hacer ahí.
  { hash: '#/panel', texto: 'Operación', icono: 'operacion', pintar: panel, operacion: true },
];

const visibles = () => SECCIONES.filter((s) => !s.operacion || modoOperacion());

for (const nodo of document.querySelectorAll('[data-icono]')) {
  nodo.append(icono(nodo.dataset.icono, nodo.classList.contains('virtud-icono') ? 20 : 18));
}

let config = null;

const formAcceso = document.getElementById('form-acceso');
const errorAcceso = document.getElementById('acceso-error');
const formAlta = document.getElementById('form-alta');
const errorAlta = document.getElementById('alta-error');
const campoAlias = document.getElementById('alta-alias');
const ayudaAlias = document.getElementById('alta-ayuda');
const AYUDA_ALIAS = ayudaAlias.textContent;

function mostrar(nodo, texto) {
  nodo.textContent = texto;
  nodo.hidden = !texto;
}

function mostrarAcceso(error = '') {
  aplicacion.hidden = true;
  acceso.hidden = false;
  formAlta.hidden = true;
  formAcceso.hidden = false;
  mostrar(errorAcceso, error);
  document.getElementById('acceso-entrar').disabled = !config;
}

function mostrarAlta() {
  aplicacion.hidden = true;
  acceso.hidden = false;
  formAcceso.hidden = true;
  formAlta.hidden = false;
  mostrar(errorAlta, '');

  const email = emailDeLaSesion();
  document.getElementById('alta-quien').textContent = email
    ? `Entraste como ${email}. El alias es cómo te van a encontrar los demás para enviarte plata.`
    : 'El alias es cómo te van a encontrar los demás para enviarte plata.';
  if (!campoAlias.value) campoAlias.value = email.split('@')[0].toLowerCase().replace(/[^a-z0-9._-]/g, '').slice(0, 30);
  campoAlias.focus();
}

// Lo mismo que se le muestra a la persona: así no descubre la regla recién cuando la API dice que no.
function problemaDelAlias(alias) {
  if (alias.length < 3) return 'Tiene que tener al menos 3 caracteres';
  if (alias.length > 30) return 'Puede tener hasta 30 caracteres';
  if (!/^[a-z0-9._-]+$/.test(alias)) return 'Sólo letras minúsculas, números, punto, guion o guion bajo';
  return '';
}

campoAlias.addEventListener('input', () => {
  const limpio = campoAlias.value.toLowerCase().replace(/\s/g, '');
  if (limpio !== campoAlias.value) campoAlias.value = limpio;
  const problema = limpio ? problemaDelAlias(limpio) : '';
  campoAlias.setAttribute('aria-invalid', problema ? 'true' : 'false');
  ayudaAlias.textContent = problema || AYUDA_ALIAS;
  ayudaAlias.classList.toggle('campo-error', Boolean(problema));
});

formAcceso.addEventListener('submit', (ev) => {
  ev.preventDefault();
  if (config) irAlLogin(config);
});

formAlta.addEventListener('submit', async (ev) => {
  ev.preventDefault();
  mostrar(errorAlta, '');
  const alias = campoAlias.value.trim();
  const problema = problemaDelAlias(alias);
  if (problema) {
    mostrar(errorAlta, problema);
    campoAlias.focus();
    return;
  }
  const boton = formAlta.querySelector('button[type=submit]');
  boton.disabled = true;
  boton.textContent = 'Creando tu cuenta…';
  try {
    // El saldo inicial lo decide la API: el cliente no lo elige.
    const r = await pedir('POST', '/cuentas', { cuerpo: { alias, tipo: 'usuario' } });
    if (r.codigo === 401 || r.codigo === 0) return;
    if (r.codigo === 409 && r.datos?.codigo === 'alias_en_uso') throw new Error(`El alias @${alias} ya está en uso: probá con otro`);
    if (!r.ok && r.datos?.codigo !== 'ya_tiene_cuenta') throw new Error(mensajeDeError(r));
    usuario.olvidarAgenda();
    if (r.ok) avisar(`¡Listo! Tu cuenta @${r.datos.alias} está creada`);
    await arrancar();
  } catch (err) {
    mostrar(errorAlta, err.message);
  } finally {
    boton.disabled = false;
    boton.textContent = 'Crear cuenta';
  }
});

document.getElementById('alta-salir').addEventListener('click', salir);

const dialogoServidor = document.getElementById('dialogo-servidor');
const estadoServidor = document.getElementById('servidor-estado');
const datosServidor = document.getElementById('servidor-datos');

function datoServidor(rotulo, valor, clase = '') {
  return el('div', { clase: 'dato' }, el('dt', { texto: rotulo }), el('dd', { clase, texto: valor }));
}

async function abrirServidor() {
  datosServidor.replaceChildren(datoServidor('API', estado.base || '(sin config.json)', 'mono'));
  estadoServidor.textContent = 'Consultando…';
  dialogoServidor.showModal();
  if (!estado.base) {
    estadoServidor.textContent = 'No hay dirección de la API: falta config.json';
    return;
  }
  try {
    const datos = await consultarSalud();
    datosServidor.append(
      datoServidor('Estado', datos.estado === 'ok' ? 'Funcionando' : datos.estado),
      datoServidor('Base de datos', datos.base === 'ok' ? 'Conectada' : datos.base),
      datoServidor('Versión', datos.version, 'mono'));
    estadoServidor.textContent = datos.estado === 'ok' ? 'Todo en orden.' : 'La API responde, pero algo no está bien.';
  } catch (err) {
    estadoServidor.textContent = err.message;
  }
}

document.getElementById('acceso-servidor').addEventListener('click', abrirServidor);
document.getElementById('alta-servidor').addEventListener('click', abrirServidor);

const dialogoClaves = document.getElementById('dialogo-claves');
dialogoClaves.addEventListener('close', () => {
  if (dialogoClaves.returnValue !== 'guardar') return;
  const admin = document.getElementById('clave-admin').value.trim();
  fijarClaves({ admin, mock: document.getElementById('clave-mock').value.trim() });
  construirNav();
  if (admin) {
    avisar('Modo operación activado');
    ir('#/panel');
  } else {
    avisar('Modo operación desactivado');
    if (location.hash.startsWith('#/panel')) ir('#/inicio');
  }
});

function abrirClaves() {
  document.getElementById('clave-admin').value = estado.admin;
  document.getElementById('clave-mock').value = estado.mock;
  dialogoClaves.returnValue = '';
  dialogoClaves.showModal();
}

function salirDeOperacion() {
  fijarClaves({ admin: '', mock: '' });
  construirNav();
  avisar('Modo operación desactivado');
  ir('#/inicio');
}

// Las tarjetas del panel piden estas acciones por evento: importarlas desde panel.js haría una
// dependencia circular.
document.addEventListener('nucleo:abrir-claves', abrirClaves);
document.addEventListener('nucleo:salir-operacion', salirDeOperacion);

function tarjetaPerfil() {
  const alias = estado.cuenta?.alias || '';
  return el('div', { clase: 'perfil' },
    avatar(alias),
    el('div', { clase: 'perfil-texto' },
      el('div', { clase: 'perfil-alias', texto: `@${alias}` }),
      el('div', { clase: 'perfil-email', texto: emailDeLaSesion() })));
}

function abrirMenuPerfil(ancla) {
  const fondo = el('div', { clase: 'menu-fondo' });
  const cerrar = () => { menu.remove(); fondo.remove(); document.removeEventListener('keydown', escape); };
  const escape = (ev) => { if (ev.key === 'Escape') cerrar(); };
  const opcion = (nombreIcono, texto, fn, clase = '') => el('button', {
    clase: `opcion ${clase}`, onclick: () => { cerrar(); fn(); },
  }, icono(nombreIcono, 18), texto);

  const menu = el('div', { clase: 'menu-perfil', role: 'menu' },
    tarjetaPerfil(),
    opcion('servidor', 'Estado del servicio', abrirServidor),
    modoOperacion()
      ? opcion('operacion', 'Salir del modo operación', salirDeOperacion)
      : opcion('llave', 'Modo operación', abrirClaves),
    opcion('salir', 'Cerrar sesión', salir, 'opcion-peligro'));

  fondo.addEventListener('click', cerrar);
  document.addEventListener('keydown', escape);
  document.body.append(fondo, menu);

  const r = ancla.getBoundingClientRect();
  const arriba = r.top > window.innerHeight / 2;
  menu.style.left = `${Math.max(8, Math.min(r.left, window.innerWidth - menu.offsetWidth - 8))}px`;
  if (arriba) menu.style.bottom = `${window.innerHeight - r.top + 8}px`;
  else menu.style.top = `${r.bottom + 8}px`;
  menu.querySelector('button.opcion')?.focus();
}

function salir() {
  cerrarSesion();
  usuario.olvidarAgenda();
  if (config) irAlLogout(config);
  else mostrarAcceso();
}

alCaerSesion((codigo) => {
  usuario.olvidarAgenda();
  mostrarAcceso(codigo === 'sesion_vencida' ? 'Tu sesión venció: volvé a ingresar' : 'Tu sesión no es válida: volvé a ingresar');
});

alPerderCuenta(() => {
  usuario.olvidarAgenda();
  mostrarAlta();
});

function construirNav() {
  vaciar(nav);
  vaciar(navMovil);
  for (const s of visibles()) {
    nav.append(el('button', { dataset: { hash: s.hash }, onclick: () => ir(s.hash) }, icono(s.icono), s.texto));
    navMovil.append(el('button', { dataset: { hash: s.hash }, onclick: () => ir(s.hash) }, icono(s.icono, 22), s.texto));
  }

  const perfil = tarjetaPerfil();
  const boton = el('button', { 'aria-label': 'Tu cuenta', onclick: () => abrirMenuPerfil(boton) }, ...perfil.childNodes);
  boton.classList.add('perfil');
  lateralPie.replaceChildren(boton);

  const movil = document.getElementById('menu-perfil-movil');
  movil.replaceChildren(avatar(estado.cuenta?.alias, 'chico'));
  movil.onclick = () => abrirMenuPerfil(movil);

  marcarActiva(`#/${(location.hash || '#/inicio').slice(2).split('/')[0]}`);
}

function ir(hash) {
  if (location.hash === hash) pintar();
  else location.hash = hash;
}

function marcarActiva(hash) {
  // El detalle de una operación se ve desde Actividad: la marca queda ahí.
  const activa = hash === '#/operacion' ? '#/actividad' : hash;
  for (const b of document.querySelectorAll('[data-hash]')) {
    if (b.dataset.hash === activa) b.setAttribute('aria-current', 'page');
    else b.removeAttribute('aria-current');
  }
}

// Cada pantalla se dibuja en su propio contenedor: si una anterior termina tarde, escribe en uno
// que ya no está en la página.
async function pintar() {
  if (!vigente() || !haySesion()) return;

  const hash = location.hash || '#/inicio';
  const partes = hash.slice(2).split('/');
  const lienzo = el('div', { clase: 'entrar', style: 'display:grid;grid-template-columns:minmax(0,1fr);gap:inherit;min-width:0' });
  hoja.replaceChildren(lienzo);
  marcarActiva(`#/${partes[0]}`);
  window.scrollTo({ top: 0 });

  try {
    if (partes[0] === 'operacion') {
      await usuario.operacion(lienzo, ir, partes[1], partes[2]);
      return;
    }
    if (partes[0] === 'enviar' && partes[1]) {
      await usuario.enviar(lienzo, ir, Number(partes[1]));
      return;
    }
    const seccion = visibles().find((s) => s.hash === `#/${partes[0]}`) || SECCIONES[0];
    await seccion.pintar(lienzo, ir);
  } catch (err) {
    if (!haySesion() || !lienzo.isConnected) return;
    lienzo.replaceChildren(el('section', { clase: 'tarjeta' },
      el('div', { clase: 'vacio' },
        el('span', { clase: 'vacio-icono' }, icono('alerta', 26)),
        el('p', { clase: 'vacio-titulo', texto: 'No pudimos cargar esta pantalla' }),
        el('p', { clase: 'vacio-detalle', texto: err.message || String(err) }),
        el('div', { clase: 'fila-botones' },
          el('button', { clase: 'boton boton-primario', onclick: pintar }, icono('recargar', 18), 'Reintentar'),
          el('button', { clase: 'boton', texto: 'Estado del servicio', onclick: abrirServidor })))));
  }
}

window.addEventListener('hashchange', pintar);

let vencimiento = 0;

function entrar() {
  clearTimeout(vencimiento);
  vencimiento = setTimeout(vigente, Math.max(0, estado.expira - Date.now()) + 1000);
  acceso.hidden = true;
  aplicacion.hidden = false;
  construirNav();
  if (!location.hash.startsWith('#/') || location.hash === '#/') location.hash = '#/inicio';
  else pintar();
}

// El token guardado puede haber vencido o la cuenta haber sido borrada mientras la pestaña
// estaba cerrada: se confirma contra la API antes de mostrar nada.
async function arrancar() {
  if (!hayToken()) {
    mostrarAcceso();
    return;
  }
  let r;
  try {
    r = await pedir('GET', '/sesiones/actual');
  } catch (err) {
    mostrarAcceso(err.message);
    return;
  }
  if (r.codigo === 401 || r.codigo === 0) return;
  if (!r.ok) {
    mostrarAcceso(mensajeDeError(r));
    return;
  }
  if (!r.datos.sesion) {
    mostrarAcceso('La API corre sin sesiones: esta app necesita Cognito');
    return;
  }
  if (r.datos.cuenta_id == null) {
    mostrarAlta();
    return;
  }
  fijarCuenta({ id: r.datos.cuenta_id, alias: r.datos.alias });
  entrar();
}

async function iniciar() {
  const vuelta = leerVuelta();
  try {
    config = await leerConfig();
    fijarBase(config.api_url);
  } catch (err) {
    mostrarAcceso(err.message);
    return;
  }
  if (vuelta?.error) {
    mostrarAcceso(vuelta.error);
    return;
  }
  if (vuelta?.tokens) fijarTokens(vuelta.tokens);
  await arrancar();
}

iniciar();
