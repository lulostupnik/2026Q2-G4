import { pedir, exigir, estado, claveDeIntento, mensajeDeError } from './api.js';
import {
  el, enPesos, conSigno, aCentavos, cuando, fechaCompleta, desenlace, periodoActual,
  nombreDelPeriodo, avatar, etiquetaDia, botonCopiar, esqueleto, vacio, hora,
} from './formato.js';
import { icono } from './iconos.js';

const cuentaID = () => estado.cuenta?.id;

// Los movimientos traen contraparte_id, no un alias: el padrón traduce eso a "Para juan.perez".
// Se cachea y se vuelve a pedir sólo si aparece una cuenta que no conoce.
let padron = null;

async function agenda(requeridas = []) {
  if (!padron || requeridas.some((id) => !padron.has(id))) {
    const { cuentas } = await exigir('GET', '/cuentas?limite=200');
    padron = new Map(cuentas.map((c) => [c.id, c]));
  }
  return padron;
}

const contrapartes = (movimientos) => (movimientos || []).map((m) => m.contraparte_id);

export function olvidarAgenda() { padron = null; }

const TIPO_POR_TRAMO = {
  transferencia: 'transferencia',
  semilla: 'transferencia',
  reserva: 'egreso',
  reversa: 'egreso',
  liquidacion: 'egreso',
  ingreso_banco: 'ingreso',
  ingreso_acreditacion: 'ingreso',
};

// El ledger asienta por tramos: un retiro rechazado son dos asientos de la misma orden (la reserva
// y su devolución). A la persona se le muestra una fila por operación, con el neto, y la más
// reciente arriba.
function consolidar(movimientos) {
  const porOrden = new Map();
  for (const m of movimientos || []) {
    const previa = porOrden.get(m.orden_id);
    if (!previa) {
      porOrden.set(m.orden_id, { ...m, bruto: Math.abs(m.monto_centavos), partes: 1 });
      continue;
    }
    previa.monto_centavos += m.monto_centavos;
    previa.bruto = Math.max(previa.bruto, Math.abs(m.monto_centavos));
    previa.partes += 1;
    if (m.creado_en > previa.creado_en) previa.creado_en = m.creado_en;
  }
  return [...porOrden.values()].sort((a, b) =>
    (b.creado_en > a.creado_en) - (b.creado_en < a.creado_en) || b.orden_id - a.orden_id);
}

const anulada = (m) => m.partes > 1 && m.monto_centavos === 0;

const MONTOS_RAPIDOS = [100000, 500000, 1000000, 2500000];

// Preferencia de esta persona en este navegador: no es un dato, puede perderse sin problema.
const LLAVE_OCULTAR = 'nucleo.ocultar-saldo';
function saldoOculto() { try { return localStorage.getItem(LLAVE_OCULTAR) === '1'; } catch { return false; } }
function ocultarSaldo(si) { try { si ? localStorage.setItem(LLAVE_OCULTAR, '1') : localStorage.removeItem(LLAVE_OCULTAR); } catch { /* sin persistencia */ } }

function encabezado(titulo, sub = '', volver = null) {
  return el('div', {},
    el('div', { clase: 'encabezado' },
      volver ? el('button', { clase: 'boton-icono', 'aria-label': 'Volver', onclick: volver }, icono('atras')) : null,
      el('h1', { texto: titulo })),
    sub ? el('p', { clase: 'encabezado-sub', texto: sub }) : null);
}

function pasos(total, actual) {
  return el('div', { clase: 'pasos', 'aria-label': `Paso ${actual} de ${total}` },
    ...Array.from({ length: total }, (_, i) => el('span', { clase: i < actual ? 'hecho' : '' })));
}

async function saldoActual() {
  const s = await exigir('GET', `/cuentas/${cuentaID()}/saldo`);
  return s.saldo_vigente_centavos;
}

// ---------------------------------------------------------------- Inicio

export async function inicio(hoja, ir) {
  const importe = el('div', { clase: 'saldo-importe' }, el('span', { clase: 'esqueleto-bloque', style: 'width:9ch;opacity:.4' }));
  let valor = null;
  const pintarImporte = () => {
    if (valor === null) return;
    importe.textContent = saldoOculto() ? '$ ••••••' : enPesos(valor);
  };
  const ojo = el('button', { clase: 'boton-icono', 'aria-label': 'Mostrar u ocultar el saldo' });
  const pintarOjo = () => ojo.replaceChildren(icono(saldoOculto() ? 'ojoTachado' : 'ojo', 18));
  ojo.addEventListener('click', () => { ocultarSaldo(!saldoOculto()); pintarOjo(); pintarImporte(); });
  pintarOjo();

  const alias = estado.cuenta?.alias || '';
  const saldo = el('section', { clase: 'saldo' },
    el('div', { clase: 'saldo-rotulo' }, icono('billetera', 18), 'Dinero disponible', ojo),
    importe,
    el('div', { clase: 'saldo-pie' },
      el('span', { clase: 'saldo-alias' }, `@${alias}`, botonCopiar(alias, 'Copiar tu alias'))));

  const acciones = el('section', { clase: 'acciones' },
    accion('enviar', 'Enviar', () => ir('#/enviar')),
    accion('ingresar', 'Ingresar', () => ir('#/ingresar')),
    accion('retirar', 'Retirar', () => ir('#/retirar')),
    accion('actividad', 'Actividad', () => ir('#/actividad')));

  const lista = el('div', { clase: 'lista' }, esqueleto(4));
  const recientes = el('section', { clase: 'tarjeta' },
    el('div', { clase: 'cabecera-tarjeta' },
      el('h2', { texto: 'Movimientos recientes' }),
      el('button', { clase: 'enlace', texto: 'Ver todo', onclick: () => ir('#/actividad') })),
    lista);

  hoja.append(
    el('div', {}, el('p', { clase: 'encabezado-sub', texto: saludo() }), el('h1', { texto: `Hola, ${alias}` })),
    saldo, acciones, recientes);

  const [v, movs] = await Promise.all([
    saldoActual(),
    exigir('GET', `/cuentas/${cuentaID()}/movimientos?limite=6`),
  ]);
  valor = v;
  pintarImporte();
  const contactos = await agenda(contrapartes(movs.movimientos));
  pintarMovimientos(lista, consolidar(movs.movimientos).slice(0, 5), contactos, ir, {
    vacio: vacio('actividad', 'Todavía no hay movimientos', 'Cuando envíes, recibas o ingreses plata, lo vas a ver acá.',
      el('button', { clase: 'boton boton-primario', texto: 'Ingresar dinero', onclick: () => ir('#/ingresar') })),
  });
}

function saludo() {
  const h = new Date().getHours();
  return h < 12 ? 'Buen día' : h < 20 ? 'Buenas tardes' : 'Buenas noches';
}

function accion(nombreIcono, texto, onclick) {
  return el('button', { clase: 'accion', onclick },
    el('span', { clase: 'accion-icono' }, icono(nombreIcono, 22)), texto);
}

// ---------------------------------------------------------------- Actividad

export async function actividad(hoja, ir) {
  let filtro = 'todos';
  let ruta = `/cuentas/${cuentaID()}/movimientos?periodo=${periodoActual()}`;
  let extracto = null;
  let contactos = null;

  const nombreMes = el('span', { clase: 'mes-nombre', texto: nombreDelPeriodo(periodoActual()) });
  const anterior = el('button', { clase: 'boton-icono', 'aria-label': 'Mes anterior', disabled: true }, icono('izquierda'));
  const siguiente = el('button', { clase: 'boton-icono', 'aria-label': 'Mes siguiente', disabled: true }, icono('derecha'));
  const totales = el('div', { clase: 'totales' });
  const origen = el('div');
  const lista = el('div', { clase: 'lista' }, esqueleto(5));

  const botonesFiltro = [['todos', 'Todos'], ['entra', 'Entradas'], ['sale', 'Salidas']].map(([valor, texto]) =>
    el('button', {
      type: 'button', texto, 'aria-pressed': String(valor === filtro),
      onclick: () => {
        filtro = valor;
        botonesFiltro.forEach((b) => b.setAttribute('aria-pressed', String(b.dataset.valor === filtro)));
        pintarLista();
      },
      dataset: { valor },
    }));

  hoja.append(
    encabezado('Actividad', 'Todo lo que entró y salió de tu cuenta, mes por mes.'),
    el('section', { clase: 'tarjeta' },
      el('div', { clase: 'cabecera-tarjeta' },
        el('div', { clase: 'mes-nav' }, anterior, nombreMes, siguiente),
        el('div', { clase: 'segmentado', role: 'group', 'aria-label': 'Filtrar' }, ...botonesFiltro)),
      totales, origen, lista));

  const pintarLista = () => {
    const movs = consolidar(extracto?.movimientos).filter((m) =>
      filtro === 'todos' || (filtro === 'entra' ? m.monto_centavos > 0 : m.monto_centavos < 0));
    pintarMovimientos(lista, movs, contactos, ir, {
      agrupar: true,
      vacio: vacio('calendario', filtro === 'todos' ? 'Este mes no tuvo movimientos' : 'No hay movimientos con ese filtro'),
    });
  };

  const cargar = async () => {
    lista.replaceChildren(esqueleto(5));
    anterior.disabled = true;
    siguiente.disabled = true;
    extracto = await exigir('GET', ruta);
    contactos = await agenda(contrapartes(extracto.movimientos));
    if (!lista.isConnected) return;

    nombreMes.textContent = nombreDelPeriodo(extracto.periodo);
    const operaciones = consolidar(extracto.movimientos);
    const entro = operaciones.filter((m) => m.monto_centavos > 0).reduce((a, m) => a + m.monto_centavos, 0);
    const salio = operaciones.filter((m) => m.monto_centavos < 0).reduce((a, m) => a - m.monto_centavos, 0);
    totales.replaceChildren(
      total('Entró', enPesos(entro), 'monto-entra'),
      total('Salió', enPesos(salio)),
      total('Balance del mes', conSigno(entro - salio)));
    origen.replaceChildren(extracto.origen === 'archivo'
      ? el('div', { clase: 'nota', style: 'margin-bottom:1rem' }, icono('info', 18),
        el('span', { texto: 'Este mes ya está cerrado y archivado: los movimientos se leen del archivo histórico.' }))
      : '');

    const links = extracto._links || {};
    if (links.periodo_anterior) {
      anterior.disabled = false;
      anterior.onclick = () => { ruta = links.periodo_anterior; cargar().catch(fallo); };
    }
    if (links.periodo_siguiente && extracto.periodo < periodoActual()) {
      siguiente.disabled = false;
      siguiente.onclick = () => { ruta = links.periodo_siguiente; cargar().catch(fallo); };
    }
    pintarLista();
  };
  const fallo = (err) => lista.replaceChildren(el('p', { clase: 'error', texto: err.message }));

  await cargar();
}

function total(rotulo, valor, clase = '') {
  return el('div', { clase: 'total' },
    el('div', { clase: 'total-rotulo', texto: rotulo }),
    el('div', { clase: `total-valor ${clase}`, texto: valor }));
}

function pintarMovimientos(lista, movimientos, contactos, ir, { agrupar = false, vacio: nodoVacio } = {}) {
  lista.replaceChildren();
  if (!movimientos?.length) {
    lista.append(nodoVacio || vacio('actividad', 'Todavía no hay movimientos'));
    return;
  }
  let dia = '';
  for (const m of movimientos) {
    if (agrupar) {
      const etiqueta = etiquetaDia(m.creado_en);
      if (etiqueta !== dia) {
        dia = etiqueta;
        lista.append(el('div', { clase: 'grupo-dia', texto: etiqueta }));
      }
    }
    lista.append(filaMovimiento(m, contactos, ir, agrupar));
  }
}

function filaMovimiento(m, contactos, ir, soloHora) {
  const entra = m.monto_centavos > 0;
  const tipo = TIPO_POR_TRAMO[m.tramo] || 'transferencia';
  const otra = contactos?.get(m.contraparte_id);
  const esPersona = tipo === 'transferencia' && otra && otra.tipo !== 'interna';

  const marca = esPersona
    ? avatar(otra.alias)
    : el('span', { clase: `fila-icono ${tipo === 'transferencia' ? (entra ? 'entra' : 'sale') : 'banco'}` },
      icono(tipo === 'egreso' ? 'retirar' : tipo === 'ingreso' ? 'ingresar' : entra ? 'ingresar' : 'enviar', 20));

  // Agrupada por día alcanza la hora; suelta, "Hoy, 10:27" o la fecha corta.
  const dia = etiquetaDia(m.creado_en);
  const momento = soloHora ? hora(m.creado_en)
    : dia === 'Hoy' || dia === 'Ayer' ? `${dia}, ${hora(m.creado_en)}` : cuando(m.creado_en);
  return el('button', { clase: 'fila', onclick: () => ir(`#/operacion/${tipo}/${m.orden_id}`) },
    marca,
    el('span', { clase: 'fila-texto' },
      el('div', { clase: 'fila-titulo', texto: tituloMovimiento(m, tipo, entra, otra) }),
      el('div', { clase: 'fila-sub', texto: `${subtituloMovimiento(m, tipo, entra)} · ${momento}` })),
    anulada(m)
      ? el('span', { clase: 'fila-monto monto-anulado', texto: enPesos(m.bruto) })
      : el('span', { clase: `fila-monto ${entra ? 'monto-entra' : 'monto-sale'}`, texto: conSigno(m.monto_centavos) }));
}

function tituloMovimiento(m, tipo, entra, otra) {
  if (anulada(m)) return tipo === 'egreso' ? 'Retiro rechazado' : 'Operación anulada';
  if (tipo === 'egreso') return entra ? 'Retiro devuelto' : 'Retiro a cuenta bancaria';
  if (tipo === 'ingreso') return 'Ingreso desde tu banco';
  if (!otra || otra.tipo === 'interna') return entra ? 'Saldo de bienvenida' : 'Ajuste';
  return `@${otra.alias}`;
}

function subtituloMovimiento(m, tipo, entra) {
  if (anulada(m)) return 'Te devolvimos el dinero';
  if (tipo === 'egreso') return entra ? 'Devolución' : 'Retiro';
  if (tipo === 'ingreso') return 'Ingreso';
  return entra ? 'Recibiste' : 'Enviaste';
}

// ---------------------------------------------------------------- Enviar

export async function enviar(hoja, ir, preseleccion = 0) {
  const tarjeta = el('section', { clase: 'tarjeta' }, esqueleto(4));
  hoja.append(encabezado('Enviar dinero', 'Al instante y sin costo, a cualquier cuenta de núcleo.'), tarjeta);

  olvidarAgenda();
  const [contactos, disponible] = await Promise.all([agenda(), saldoActual()]);
  const personas = [...contactos.values()]
    .filter((c) => c.id !== cuentaID() && c.tipo !== 'interna' && c.activa !== false)
    .sort((a, b) => a.alias.localeCompare(b.alias));

  const claveEnvio = claveDeIntento('envio');
  let destino = personas.find((p) => p.id === preseleccion) || null;
  let centavos = null;

  const paso1 = () => {
    if (!personas.length) {
      tarjeta.replaceChildren(vacio('usuario', 'Todavía no hay a quién enviarle',
        'Cuando otra persona cree su cuenta, va a aparecer acá.',
        el('button', { clase: 'boton', texto: 'Volver al inicio', onclick: () => ir('#/inicio') })));
      return;
    }
    const buscar = el('input', { type: 'search', placeholder: 'Buscar por alias', 'aria-label': 'Buscar por alias', autocomplete: 'off' });
    const lista = el('div', { clase: 'lista' });
    const filtrar = () => {
      const q = buscar.value.trim().toLowerCase().replace(/^@/, '');
      const encontrados = personas.filter((p) => p.alias.toLowerCase().includes(q));
      lista.replaceChildren(...(encontrados.length ? encontrados.map((p) => el('button', {
        clase: 'fila', onclick: () => { destino = p; paso2(); },
      }, avatar(p.alias), el('span', { clase: 'fila-texto' },
        el('div', { clase: 'fila-titulo', texto: `@${p.alias}` }),
        el('div', { clase: 'fila-sub', texto: p.tipo === 'comercio' ? 'Comercio' : 'Persona' })),
      icono('derecha', 18))) : [vacio('buscar', `No encontramos a "${buscar.value}"`, 'Revisá cómo está escrito el alias.')]));
      for (const f of lista.querySelectorAll('.fila > .ico')) f.classList.add('ico-fin');
    };
    buscar.addEventListener('input', filtrar);
    filtrar();
    tarjeta.replaceChildren(pasos(3, 1),
      el('h2', { texto: '¿A quién le enviás?', style: 'margin-bottom:.9rem' }),
      el('label', { clase: 'buscador' }, icono('buscar', 18), buscar),
      lista);
    buscar.focus();
  };

  const paso2 = () => {
    const entrada = el('input', {
      clase: 'importe-entrada', inputMode: 'decimal', placeholder: '$ 0', autocomplete: 'off',
      'aria-label': 'Monto', value: centavos ? String(centavos / 100).replace('.', ',') : '',
    });
    const problema = el('p', { clase: 'campo-error', hidden: true, role: 'alert' });
    const continuar = el('button', { clase: 'boton boton-primario boton-ancho boton-grande', type: 'submit', texto: 'Continuar' });

    const validar = () => {
      const c = aCentavos(entrada.value);
      let error = '';
      if (entrada.value.trim() && c === null) error = 'Escribí un monto válido, por ejemplo 1500 o 1.500,50';
      else if (c !== null && c <= 0) error = 'El monto tiene que ser mayor a cero';
      else if (c !== null && c > disponible) error = `No te alcanza: tenés ${enPesos(disponible)} disponibles`;
      problema.textContent = error;
      problema.hidden = !error;
      continuar.disabled = Boolean(error) || c === null;
      return error ? null : c;
    };
    entrada.addEventListener('input', validar);

    const form = el('form', { novalidate: true, style: 'display:grid;gap:1rem' },
      cabeceraDestino(destino, paso1),
      el('div', { clase: 'importe-campo' },
        entrada,
        el('p', { clase: 'importe-pie' }, 'Disponible: ', el('strong', { texto: enPesos(disponible) })),
        chips(entrada, validar)),
      problema, continuar);
    form.addEventListener('submit', (ev) => {
      ev.preventDefault();
      const c = validar();
      if (c) { centavos = c; paso3(); }
    });
    tarjeta.replaceChildren(pasos(3, 2), form);
    validar();
    entrada.focus();
  };

  const paso3 = () => {
    const error = el('p', { clase: 'error', hidden: true, role: 'alert' });
    const confirmar = el('button', { clase: 'boton boton-primario boton-ancho boton-grande', texto: `Enviar ${enPesos(centavos)}` });
    confirmar.addEventListener('click', async () => {
      confirmar.disabled = true;
      confirmar.textContent = 'Enviando…';
      error.hidden = true;
      try {
        const cuerpo = { origen_id: cuentaID(), destino_id: destino.id, monto_centavos: centavos };
        const r = await pedir('POST', '/transferencias', { clave: claveEnvio(cuerpo), cuerpo });
        if (r.codigo === 0 && r.datos?.codigo === 'sesion_vencida') return;
        tarjeta.replaceChildren(resultado(r, ir, {
          exito: `Le enviaste dinero a @${destino.alias}`, centavos, detalle: 'Ya lo tiene disponible en su cuenta.',
        }));
      } catch (err) {
        // Sin respuesta no se sabe si salió: la misma Idempotency-Key hace seguro reintentar.
        error.textContent = `${err.message}. Podés reintentar sin riesgo de enviarlo dos veces.`;
        error.hidden = false;
        confirmar.disabled = false;
        confirmar.textContent = 'Reintentar';
      }
    });
    tarjeta.replaceChildren(pasos(3, 3),
      el('div', { clase: 'resultado', style: 'padding-bottom:.5rem' },
        avatar(destino.alias, 'grande'),
        el('p', { clase: 'ayuda', texto: 'Vas a enviarle a' }),
        el('h2', { texto: `@${destino.alias}` }),
        el('div', { clase: 'importe-grande', texto: enPesos(centavos) })),
      el('dl', { clase: 'recibo' },
        filaRecibo('Desde', `Tu cuenta @${estado.cuenta?.alias}`),
        filaRecibo('Costo', 'Sin costo'),
        filaRecibo('Acreditación', 'Inmediata'),
        filaRecibo('Te queda', enPesos(disponible - centavos))),
      el('div', { style: 'display:grid;gap:.6rem;margin-top:1rem' },
        confirmar,
        el('button', { clase: 'boton boton-fantasma boton-ancho', texto: 'Cambiar el monto', onclick: paso2 })),
      error);
    confirmar.focus();
  };

  if (destino) paso2(); else paso1();
}

function cabeceraDestino(destino, cambiar) {
  return el('div', { clase: 'destinatario' },
    avatar(destino.alias),
    el('div', { clase: 'destinatario-texto' },
      el('div', { clase: 'destinatario-rotulo', texto: 'Para' }),
      el('div', { clase: 'destinatario-alias', texto: `@${destino.alias}` })),
    el('button', { type: 'button', clase: 'enlace', texto: 'Cambiar', onclick: cambiar }));
}

function chips(entrada, alCambiar, extra = []) {
  return el('div', { clase: 'chips' },
    ...MONTOS_RAPIDOS.map((c) => el('button', {
      type: 'button', clase: 'chip', texto: enPesos(c).replace(/,00$/, ''),
      onclick: () => { entrada.value = String(c / 100); alCambiar(); entrada.focus(); },
    })),
    ...extra);
}

// ---------------------------------------------------------------- Retirar

const soloDigitos = (t) => String(t || '').replace(/\D/g, '');
const cbuVisible = (cbu) => cbu.replace(/(\d{4})(?=\d)/g, '$1 ');

export async function retirar(hoja, ir) {
  const tarjeta = el('section', { clase: 'tarjeta' }, esqueleto(3));
  hoja.append(encabezado('Retirar a tu banco', 'Pasá plata de núcleo a cualquier cuenta bancaria o virtual.'), tarjeta);

  const disponible = await saldoActual();
  const claveRetiro = claveDeIntento('retiro');
  let cbu = '';
  let centavos = null;

  const paso1 = () => {
    const campoCbu = el('input', {
      inputMode: 'numeric', autocomplete: 'off', placeholder: '0000 0031 0001 0000 0000 01',
      'aria-describedby': 'cbu-ayuda', value: cbuVisible(cbu), maxLength: 27,
    });
    const ayudaCbu = el('span', { clase: 'campo-ayuda', id: 'cbu-ayuda', texto: 'Los 22 números de la CBU o CVU de destino.' });
    const entrada = el('input', {
      clase: 'importe-entrada', inputMode: 'decimal', placeholder: '$ 0', autocomplete: 'off',
      'aria-label': 'Monto', value: centavos ? String(centavos / 100).replace('.', ',') : '',
    });
    const problema = el('p', { clase: 'campo-error', hidden: true, role: 'alert' });
    const continuar = el('button', { clase: 'boton boton-primario boton-ancho boton-grande', type: 'submit', texto: 'Continuar' });

    const validar = () => {
      const digitos = soloDigitos(campoCbu.value);
      const errorCbu = digitos && digitos.length !== 22 ? `Tiene ${digitos.length} números: una CBU o CVU tiene 22` : '';
      campoCbu.setAttribute('aria-invalid', String(Boolean(errorCbu)));
      ayudaCbu.textContent = errorCbu || 'Los 22 números de la CBU o CVU de destino.';
      ayudaCbu.className = errorCbu ? 'campo-error' : 'campo-ayuda';

      const c = aCentavos(entrada.value);
      let error = '';
      if (entrada.value.trim() && c === null) error = 'Escribí un monto válido, por ejemplo 1500 o 1.500,50';
      else if (c !== null && c <= 0) error = 'El monto tiene que ser mayor a cero';
      else if (c !== null && c > disponible) error = `No te alcanza: tenés ${enPesos(disponible)} disponibles`;
      problema.textContent = error;
      problema.hidden = !error;
      continuar.disabled = Boolean(error || errorCbu) || c === null || digitos.length !== 22;
      return { c, digitos };
    };
    campoCbu.addEventListener('input', () => {
      const pos = campoCbu.value.length;
      campoCbu.value = cbuVisible(soloDigitos(campoCbu.value).slice(0, 22));
      if (pos >= campoCbu.value.length) campoCbu.setSelectionRange(campoCbu.value.length, campoCbu.value.length);
      validar();
    });
    entrada.addEventListener('input', validar);

    const todo = el('button', {
      type: 'button', clase: 'chip', texto: 'Todo',
      onclick: () => { entrada.value = String(disponible / 100).replace('.', ','); validar(); },
    });

    const form = el('form', { novalidate: true, style: 'display:grid;gap:1rem' },
      el('label', { clase: 'campo' }, el('span', { clase: 'campo-rotulo', texto: 'CBU o CVU de destino' }), campoCbu, ayudaCbu),
      el('div', { clase: 'importe-campo' },
        entrada,
        el('p', { clase: 'importe-pie' }, 'Disponible: ', el('strong', { texto: enPesos(disponible) })),
        chips(entrada, validar, [todo])),
      problema, continuar);
    form.addEventListener('submit', (ev) => {
      ev.preventDefault();
      const { c, digitos } = validar();
      if (c && c > 0 && c <= disponible && digitos.length === 22) { cbu = digitos; centavos = c; paso2(); }
    });
    tarjeta.replaceChildren(pasos(2, 1), form);
    validar();
    (cbu ? entrada : campoCbu).focus();
  };

  const paso2 = () => {
    const error = el('p', { clase: 'error', hidden: true, role: 'alert' });
    const confirmar = el('button', { clase: 'boton boton-primario boton-ancho boton-grande', texto: `Retirar ${enPesos(centavos)}` });
    confirmar.addEventListener('click', async () => {
      confirmar.disabled = true;
      confirmar.textContent = 'Procesando…';
      error.hidden = true;
      try {
        const cuerpo = { origen_id: cuentaID(), cbu_destino: cbu, monto_centavos: centavos };
        const r = await pedir('POST', '/egresos', { clave: claveRetiro(cuerpo), cuerpo });
        if (r.codigo === 0 && r.datos?.codigo === 'sesion_vencida') return;
        tarjeta.replaceChildren(resultado(r, ir, {
          exito: 'Tu retiro está en camino', centavos, detalle: `A la cuenta terminada en ${cbu.slice(-4)}.`,
        }));
      } catch (err) {
        error.textContent = `${err.message}. Podés reintentar sin riesgo de retirarlo dos veces.`;
        error.hidden = false;
        confirmar.disabled = false;
        confirmar.textContent = 'Reintentar';
      }
    });
    tarjeta.replaceChildren(pasos(2, 2),
      el('div', { clase: 'resultado', style: 'padding-bottom:.5rem' },
        el('span', { clase: 'sello sello-espera' }, icono('retirar', 30)),
        el('p', { clase: 'ayuda', texto: 'Vas a retirar' }),
        el('div', { clase: 'importe-grande', texto: enPesos(centavos) })),
      el('dl', { clase: 'recibo' },
        filaRecibo('A la cuenta', cbuVisible(cbu), 'mono'),
        filaRecibo('Costo', 'Sin costo'),
        filaRecibo('Te queda', enPesos(disponible - centavos))),
      el('div', { clase: 'nota', style: 'margin-top:1rem' }, icono('reloj', 18),
        el('span', { texto: 'Lo procesa el banco: normalmente tarda unos segundos. Si demora, te avisamos en la actividad.' })),
      el('div', { style: 'display:grid;gap:.6rem;margin-top:1rem' },
        confirmar,
        el('button', { clase: 'boton boton-fantasma boton-ancho', texto: 'Modificar', onclick: paso1 })),
      error);
    confirmar.focus();
  };

  paso1();
}

// ---------------------------------------------------------------- Ingresar

export async function ingresar(hoja, ir) {
  const tarjeta = el('section', { clase: 'tarjeta' });
  hoja.append(encabezado('Ingresar dinero', 'Transferí desde tu banco y lo acreditamos en tu cuenta.'), tarjeta);

  const claveIngreso = claveDeIntento('ingreso');

  const entrada = el('input', {
    clase: 'importe-entrada', inputMode: 'decimal', placeholder: '$ 0', autocomplete: 'off', 'aria-label': 'Monto',
  });
  const problema = el('p', { clase: 'campo-error', hidden: true, role: 'alert' });
  const continuar = el('button', { clase: 'boton boton-primario boton-ancho boton-grande', type: 'submit', texto: 'Continuar' });

  const validar = () => {
    const c = aCentavos(entrada.value);
    let error = '';
    if (entrada.value.trim() && c === null) error = 'Escribí un monto válido, por ejemplo 1500 o 1.500,50';
    else if (c !== null && c <= 0) error = 'El monto tiene que ser mayor a cero';
    problema.textContent = error;
    problema.hidden = !error;
    continuar.disabled = Boolean(error) || c === null;
    return error ? null : c;
  };
  entrada.addEventListener('input', validar);

  const form = el('form', { novalidate: true, style: 'display:grid;gap:1rem' },
    el('h2', { texto: '¿Cuánto querés ingresar?' }),
    el('div', { clase: 'importe-campo' }, entrada, chips(entrada, validar)),
    problema, continuar);
  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    const centavos = validar();
    if (!centavos) return;
    continuar.disabled = true;
    continuar.textContent = 'Generando código…';
    try {
      const cuerpo = { destino_id: cuentaID(), monto_centavos: centavos };
      const orden = await exigir('POST', '/ingresos', { clave: claveIngreso(cuerpo), cuerpo });
      tarjeta.replaceChildren(instruccionesDeIngreso(orden, centavos, ir));
    } catch (err) {
      problema.textContent = err.message;
      problema.hidden = false;
      continuar.disabled = false;
      continuar.textContent = 'Continuar';
    }
  });
  tarjeta.replaceChildren(pasos(2, 1), form);
  validar();
  entrada.focus();
}

// "Ya hice la transferencia" hace de aviso del banco: la API registra el depósito en el
// proveedor vigente y concilia en la misma llamada.
function instruccionesDeIngreso(orden, centavos, ir) {
  const caja = el('div');
  const estadoLinea = el('p', { clase: 'ayuda', role: 'status', style: 'text-align:center' });

  // El depósito se registra una sola vez; los reintentos sólo consultan la orden.
  let depositado = false;
  const buscar = async () => {
    if (depositado) return exigir('GET', `/ingresos/${orden.id}`);
    const r = await pedir('POST', `/ingresos/${orden.id}/simular-deposito`);
    if (r.codigo === 409 && r.datos?.codigo === 'ingreso_no_pendiente') {
      depositado = true;
      return exigir('GET', `/ingresos/${orden.id}`);
    }
    if (!r.ok) throw new Error(mensajeDeError(r));
    depositado = true;
    return r.datos;
  };

  const confirmar = el('button', {
    clase: 'boton boton-primario boton-ancho boton-grande',
    texto: 'Ya hice la transferencia',
    onclick: async () => {
      confirmar.disabled = true;
      confirmar.textContent = 'Buscando tu transferencia…';
      estadoLinea.textContent = '';
      try {
        const actualizada = await buscar();
        if (actualizada.estado === 'pendiente') {
          estadoLinea.textContent = 'Todavía no la vemos. Probá de nuevo en unos segundos.';
          confirmar.disabled = false;
          confirmar.textContent = 'Volver a buscar';
        } else {
          caja.replaceChildren(resultado({ ok: true, codigo: 200, datos: actualizada }, ir,
            { exito: 'Ya tenés el dinero en tu cuenta', centavos }));
        }
      } catch (err) {
        estadoLinea.textContent = err.message;
        confirmar.disabled = false;
        confirmar.textContent = 'Ya hice la transferencia';
      }
    },
  });

  caja.append(
    pasos(2, 2),
    el('h2', { texto: 'Transferí desde tu banco' }),
    el('ol', { clase: 'instrucciones' },
      el('li', {}, el('span', {}, 'Abrí la app o el home banking de tu banco y hacé una transferencia por ',
        el('strong', { texto: enPesos(centavos) }), '.')),
      el('li', {}, el('span', { texto: 'En el concepto o la referencia, escribí este código. Así sabemos que la plata es tuya:' })),
    ),
    el('div', { clase: 'codigo-grande' }, el('span', { texto: orden.referencia }), botonCopiar(orden.referencia, 'Copiar el código')),
    el('ol', { clase: 'instrucciones', start: 3, style: 'counter-reset: paso 2' },
      el('li', {}, el('span', { texto: 'Cuando la hayas hecho, avisanos con el botón de abajo.' }))),
    el('div', { clase: 'nota nota-espera', style: 'margin-bottom:1rem' }, icono('info', 18),
      el('span', { texto: 'En esta demo no hace falta ir al banco: el botón simula el aviso de que la transferencia llegó.' })),
    confirmar,
    estadoLinea,
    el('div', { clase: 'fila-botones', style: 'justify-content:center;margin-top:.75rem' },
      el('button', { clase: 'enlace enlace-tenue', texto: 'Lo hago más tarde', onclick: () => ir('#/inicio') })));
  return caja;
}

// ---------------------------------------------------------------- Detalle

export async function operacion(hoja, ir, tipo, id) {
  const ruta = { transferencia: 'transferencias', egreso: 'egresos', ingreso: 'ingresos' }[tipo] || 'transferencias';
  const tarjeta = el('section', { clase: 'tarjeta' }, esqueleto(4));
  hoja.append(encabezado('Detalle de la operación', '', () => history.length > 1 ? history.back() : ir('#/actividad')), tarjeta);

  const o = await exigir('GET', `/${ruta}/${id}`);
  const ids = [o.origen_id, o.destino_id].filter(Boolean);
  const contactos = await agenda(ids);
  const d = desenlace(o.estado);
  const entra = o.destino_id === cuentaID();
  const otra = contactos.get(entra ? o.origen_id : o.destino_id);
  const persona = otra && otra.tipo !== 'interna' ? otra : null;

  const titulo = o.tipo === 'egreso' ? 'Retiro a cuenta bancaria'
    : o.tipo === 'ingreso' ? 'Ingreso desde tu banco'
      : persona ? `${entra ? 'Recibiste de' : 'Enviaste a'} @${persona.alias}`
        : entra ? 'Saldo de bienvenida' : 'Ajuste';

  const pildora = el('span', { clase: `pildora pildora-${d.tono || 'espera'}` },
    icono(d.tono === 'ok' ? 'ok' : d.tono === 'no' ? 'cruz' : 'reloj', 14), d.texto);

  tarjeta.replaceChildren(
    el('div', { clase: 'resultado' },
      persona ? avatar(persona.alias, 'grande') : el('span', { clase: `sello sello-${d.tono || 'espera'}` },
        icono(o.tipo === 'egreso' ? 'retirar' : o.tipo === 'ingreso' ? 'ingresar' : 'billetera', 30)),
      el('h2', { texto: titulo }),
      el('div', { clase: `importe-grande ${entra ? 'monto-entra' : ''}`, texto: conSigno(entra ? o.monto_centavos : -o.monto_centavos) }),
      pildora),
    el('dl', { clase: 'recibo' },
      filaRecibo('Fecha', fechaCompleta(o.creada_en)),
      filaRecibo('Número de operación', `#${o.id}`, 'mono', String(o.id)),
      o.referencia ? filaRecibo(o.tipo === 'ingreso' ? 'Código de ingreso' : 'Referencia', o.referencia, 'mono', o.referencia) : null,
      o.cbu_destino ? filaRecibo('Cuenta de destino', cbuVisible(o.cbu_destino), 'mono') : null,
      o.motivo ? filaRecibo('Detalle', o.motivo) : null),
    el('div', { clase: 'fila-botones', style: 'margin-top:1.25rem' },
      // La app no decide qué acciones existen: "resolver" sólo viene si la operación quedó en duda.
      o._links?.resolver
        ? el('button', { clase: 'boton boton-primario', onclick: () => ir(`#/operacion/${tipo}/${id}`) }, icono('recargar', 18), 'Actualizar estado')
        : null,
      persona && !entra && o.tipo === 'transferencia'
        ? el('button', { clase: 'boton', onclick: () => ir(`#/enviar/${persona.id}`) }, icono('enviar', 18), 'Enviar de nuevo')
        : null,
      el('button', { clase: 'boton boton-fantasma', texto: 'Ir a la actividad', onclick: () => ir('#/actividad') })));
}

function filaRecibo(rotulo, valor, clase = '', copiable = '') {
  return el('div', { clase: 'recibo-fila' },
    el('dt', { texto: rotulo }),
    el('dd', { clase }, el('span', { texto: valor ?? '—' }), copiable ? botonCopiar(copiable) : null));
}

// Un 202 es una operación despachada cuyo desenlace no se conoce: no se dice "listo" ni
// "error", se dice que se está procesando.
function resultado(r, ir, { exito, centavos, detalle = '' }) {
  const o = r.datos || {};
  const d = desenlace(o.estado);

  const enRevision = r.codigo === 202 || o.estado === 'en_duda';
  const bien = r.ok && !enRevision && d.tono === 'ok';
  const tono = bien ? 'ok' : enRevision ? 'espera' : 'no';

  const titulo = bien ? exito
    : enRevision ? 'Estamos procesando tu operación'
      : 'No pudimos completar la operación';
  const motivo = String(o.motivo || (r.ok ? d.texto : mensajeDeError(r)));
  const explicacion = bien ? detalle
    : enRevision ? 'El banco todavía no confirmó. No hace falta que hagas nada: vas a ver el resultado en tu actividad.'
      : `${motivo.charAt(0).toUpperCase()}${motivo.slice(1)}.${o.id ? ' No se movió dinero de tu cuenta.' : ''}`;

  return el('div', {},
    el('div', { clase: 'resultado' },
      el('span', { clase: `sello sello-${tono}` }, icono(bien ? 'ok' : enRevision ? 'reloj' : 'cruz', 34)),
      el('h2', { texto: titulo }),
      centavos ? el('div', { clase: 'importe-grande', texto: enPesos(centavos) }) : null,
      explicacion ? el('p', { clase: 'ayuda', texto: explicacion }) : null,
      o.id ? el('p', { clase: 'ayuda mono', texto: `Operación #${o.id}` }) : null),
    el('div', { clase: 'fila-botones', style: 'justify-content:center' },
      el('button', { clase: 'boton boton-primario', texto: 'Volver al inicio', onclick: () => ir('#/inicio') }),
      o.id ? el('button', { clase: 'boton', texto: 'Ver comprobante', onclick: () => ir(`#/operacion/${o.tipo}/${o.id}`) }) : null));
}
