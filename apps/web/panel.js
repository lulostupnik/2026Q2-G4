import {
  pedir, exigir, estado, consultarSalud, verCinta, alAnotar, limpiarCinta, mensajeDeError,
} from './api.js';
import { el, enPesos, fechaCompleta, cuando, avisar, periodoActual, vacio } from './formato.js';
import { icono } from './iconos.js';

export async function panel(hoja) {
  const salud = tarjeta('Estado del despliegue');
  const huellas = tarjeta('Secretos compartidos');
  const proveedor = tarjeta('Proveedor de infraestructura financiera');
  const pedidos = tarjeta('Lo que me pidieron como proveedor');
  const avisos = tarjeta('Avisos de webhook recibidos');
  const contable = tarjeta('Cierre contable y archivado');
  const extracto = tarjeta('Extracto y exportación');
  const reinicio = tarjeta('Reiniciar los datos de la demo');
  const cinta = tarjetaCinta();

  hoja.append(
    el('div', {},
      el('h1', { texto: 'Operación' }),
      el('p', { clase: 'encabezado-sub', texto: 'Herramientas para quien opera el despliegue: actúan sobre todas las cuentas.' })),
    el('div', { clase: 'operacion-banda', role: 'note' },
      icono('llave', 20),
      el('p', { texto: 'Estás en modo operación. Las claves se borran al cerrar sesión.' }),
      el('button', {
        clase: 'boton boton-chico', texto: 'Salir del modo operación',
        onclick: () => document.dispatchEvent(new CustomEvent('nucleo:salir-operacion')),
      })),
    el('div', { clase: 'panel' }, salud.caja, huellas.caja, proveedor.caja,
      pedidos.caja, avisos.caja, contable.caja, extracto.caja, reinicio.caja, cinta));

  await Promise.all([
    pintarSalud(salud),
    pintarHuellas(huellas),
    pintarProveedor(proveedor),
    pintarPedidos(pedidos),
    pintarAvisos(avisos),
    pintarContable(contable),
    pintarExtracto(extracto),
    pintarReinicio(reinicio),
  ]);
}

function tarjeta(titulo, ...acciones) {
  const cuerpo = el('div');
  const caja = el('section', { clase: 'tarjeta' },
    el('div', { clase: 'cabecera-tarjeta' },
      el('h2', { texto: titulo }),
      el('div', { clase: 'fila-botones' }, ...acciones)),
    cuerpo);
  return { caja, cuerpo };
}

function dato(rotulo, valor, clase = '') {
  return el('div', { clase: 'dato' },
    el('dt', { texto: rotulo }),
    el('dd', { clase, texto: valor ?? '—' }));
}

function accion(texto, fn, clase = 'boton boton-chico') {
  const b = el('button', { clase, texto });
  b.addEventListener('click', async () => {
    b.disabled = true;
    try {
      await fn();
    } catch (err) {
      avisar(err.message || String(err), true);
    } finally {
      b.disabled = false;
    }
  });
  return b;
}

function error(cuerpo, err) {
  cuerpo.replaceChildren(el('p', { clase: 'error', texto: err.message || String(err) }));
}

function faltaClave(cuerpo, cual) {
  if (estado[cual]) return false;

  const nombre = cual === 'admin' ? 'de administración' : 'del proveedor simulado';
  cuerpo.replaceChildren(vacio('llave', `Falta la clave ${nombre}`, 'Cargala en Modo operación para usar esta tarjeta.',
    el('button', {
      clase: 'boton boton-chico',
      texto: 'Cargar clave',
      onclick: () => document.dispatchEvent(new CustomEvent('nucleo:abrir-claves')),
    })));
  return true;
}

async function pintarSalud({ cuerpo }) {
  try {
    const datos = await consultarSalud();
    cuerpo.replaceChildren(el('div', { clase: 'rejilla' },
      dato('Estado', datos.estado),
      dato('Base de datos', datos.base),
      dato('Versión', datos.version, 'mono'),
      dato('Tarea que atendió', datos.instancia, 'mono'),
      dato('Tiempo activa', datos.activo)));
  } catch (err) { error(cuerpo, err); }
}

async function pintarHuellas({ cuerpo }) {
  if (faltaClave(cuerpo, 'admin')) return;

  const refrescar = accion('Refrescar', () => pintarHuellas({ cuerpo }));

  try {
    const r = await pedir('GET', '/admin/diagnostico', { admin: true });
    if (!r.ok) throw new Error(mensajeDeError(r));
    const d = r.datos;

    // Sólo si el proveedor es el mock propio las dos claves tienen que ser iguales; con otro
    // despliegue cada una se compara con la huella del otro equipo.
    const propio = sinBarra(d.proveedor?.url) === `${estado.base}/servicio-mock`;
    const iguales = d.huellas.mock_api_key.huella === d.huellas.proveedor_api_key.huella;
    const veredicto = !propio
      ? { tono: '', texto: 'el proveedor es otro despliegue: compará estas huellas con las suyas' }
      : iguales
        ? { tono: 'ok', texto: 'el proveedor es el mock propio y usan el mismo secreto' }
        : { tono: 'no', texto: 'el proveedor es el mock propio y las claves NO coinciden: los egresos se van a rechazar' };

    const filas = Object.entries(d.huellas).map(([nombre, h]) => el('tr', {},
      el('td', { clase: 'mono', texto: nombre }),
      el('td', { clase: 'mono', texto: h.huella }),
      el('td', { texto: h.igual_a })));

    cuerpo.replaceChildren(
      el('p', { clase: 'ayuda', texto: `Atendió ${d.instancia} · activa hace ${d.activo}` }),
      el('div', { style: 'height:.6rem' }),
      el('div', { clase: 'tabla-ancha' },
        el('table', {},
          el('thead', {}, el('tr', {},
            el('th', { texto: 'Variable' }), el('th', { texto: 'Huella' }),
            el('th', { texto: 'Tiene que coincidir con' }))),
          el('tbody', {}, ...filas))),
      el('div', { style: 'height:.75rem' }),
      el('p', {},
        el('span', {
          clase: `pildora${veredicto.tono ? ` pildora-${veredicto.tono}` : ''}`,
          texto: veredicto.texto,
        })),
      el('div', { style: 'height:.75rem' }),
      el('div', { clase: 'fila-botones' }, refrescar));
  } catch (err) { error(cuerpo, err); }
}

const sinBarra = (url) => String(url || '').replace(/\/+$/, '');

async function pintarProveedor({ cuerpo }) {
  if (faltaClave(cuerpo, 'admin')) return;

  const url = el('input', { placeholder: 'http://alb-del-otro-equipo/servicio-mock' });

  const aplicar = accion('Apuntar acá', async () => {
    await exigir('POST', '/admin/proveedor', { admin: true, cuerpo: { url: url.value.trim() } });
    avisar('Proveedor cambiado. El próximo egreso ya va al nuevo.');
    await pintarProveedor({ cuerpo });
  }, 'boton boton-primario boton-chico');

  const restaurar = accion('Volver al de siempre', async () => {
    await exigir('DELETE', '/admin/proveedor', { admin: true });
    avisar('Proveedor restaurado a PROVEEDOR_URL');
    await pintarProveedor({ cuerpo });
  });

  try {
    const r = await pedir('GET', '/admin/proveedor', { admin: true });
    if (!r.ok) throw new Error(mensajeDeError(r));
    const p = r.datos;
    url.value = p.url || '';
    cuerpo.replaceChildren(
      el('div', { clase: 'rejilla' },
        dato('Apuntando a', p.url, 'mono'),
        dato('Origen', p.por_defecto ? 'PROVEEDOR_URL' : `cambiado desde ${p.origen || '—'}`),
        dato('Desde', p.actualizado_en ? fechaCompleta(p.actualizado_en) : 'el arranque')),
      el('div', { style: 'height:.75rem' }),
      el('label', { clase: 'campo' }, el('span', { texto: 'Cambiar en vivo' }), url),
      el('div', { clase: 'fila-botones' }, aplicar, restaurar));
  } catch (err) { error(cuerpo, err); }
}

async function pintarPedidos({ cuerpo }) {
  if (faltaClave(cuerpo, 'mock')) return;

  try {
    const r = await pedir('GET', '/servicio-mock/pedidos?limite=30', { mock: true, sinSesion: true });
    if (!r.ok) throw new Error(mensajeDeError(r));
    const pedidos = r.datos.pedidos || [];

    if (!pedidos.length) {
      cuerpo.replaceChildren(el('p', { clase: 'vacio', texto: 'Nadie te pidió ningún egreso todavía' }));
      return;
    }

    const filas = pedidos.map((p) => el('tr', {},
      el('td', { clase: 'mono', texto: p.referencia }),
      el('td', { clase: 'mono', texto: p.cbu }),
      el('td', { clase: 'num', texto: enPesos(p.monto_centavos) }),
      el('td', {}, el('span', {
        clase: `pildora pildora-${p.sin_respuesta ? 'espera' : p.estado === 'acreditado' ? 'ok' : 'no'}`,
        texto: p.sin_respuesta ? 'ejecutado, sin responder' : p.estado,
      })),
      el('td', { texto: cuando(p.creado_en) })));

    cuerpo.replaceChildren(el('div', { clase: 'tabla-ancha' },
      el('table', {},
        el('thead', {}, el('tr', {},
          el('th', { texto: 'Referencia' }), el('th', { texto: 'CBU' }),
          el('th', { clase: 'num', texto: 'Monto' }), el('th', { texto: 'Desenlace' }),
          el('th', { texto: 'Cuándo' }))),
        el('tbody', {}, ...filas))));
  } catch (err) { error(cuerpo, err); }
}

async function pintarAvisos({ cuerpo }) {
  if (faltaClave(cuerpo, 'admin')) return;

  try {
    const { avisos } = await exigir('GET', '/webhooks/recibidos?limite=30', { admin: true, sinSesion: true });

    if (!avisos.length) {
      cuerpo.replaceChildren(el('p', { clase: 'vacio', texto: 'Todavía no llegó ningún aviso' }));
      return;
    }

    const filas = avisos.map((a) => el('tr', {},
      el('td', { clase: 'num mono', texto: String(a.orden_id) }),
      el('td', { texto: a.tipo }),
      el('td', {}, el('span', { clase: 'pildora', texto: a.estado })),
      el('td', { clase: 'num', texto: enPesos(a.monto_centavos) }),
      el('td', {}, el('span', {
        clase: `pildora pildora-${a.firma_valida ? 'ok' : 'no'}`,
        texto: a.firma_valida ? 'firma válida' : 'firma inválida',
      })),
      el('td', { texto: cuando(a.recibido_en) })));

    cuerpo.replaceChildren(el('div', { clase: 'tabla-ancha' },
      el('table', {},
        el('thead', {}, el('tr', {},
          el('th', { clase: 'num', texto: 'Orden' }), el('th', { texto: 'Tipo' }),
          el('th', { texto: 'Estado' }), el('th', { clase: 'num', texto: 'Monto' }),
          el('th', { texto: 'Firma' }), el('th', { texto: 'Cuándo' }))),
        el('tbody', {}, ...filas))));
  } catch (err) { error(cuerpo, err); }
}

async function pintarContable({ cuerpo }) {
  if (faltaClave(cuerpo, 'admin')) return;

  const salida = el('div');

  const verificar = accion('Verificar cierre', async () => {
    const c = (await exigir('POST', '/admin/cierre', { admin: true })) || {};
    // replaceChildren, a diferencia de el(), no descarta los null: los pinta como texto.
    const partes = [
      el('div', { clase: 'rejilla' },
        dato('¿Cierra?', c.cierra ? 'sí' : 'no'),
        dato('Descuadres', String(c.descuadres?.length ?? 0)),
        ...(c.transitorias || []).map((t) => dato(t.alias, enPesos(t.saldo_centavos)))),
    ];
    if (c.descuadres?.length) {
      partes.push(el('pre', { texto: JSON.stringify(c.descuadres, null, 2) }));
    }
    salida.replaceChildren(...partes);
  });

  const archivar = accion('Agendar archivado', async () => {
    const a = await exigir('POST', '/admin/archivado?retencion_meses=0', { admin: true });
    avisar(a.agendado ? `Archivado agendado para ${a.periodo}` : a.motivo);
  });

  const semilla = accion('Sembrar mes anterior', async () => {
    // La semilla escribe por el mismo camino que una transferencia, así que el origen tiene
    // que tener el dinero, y la única cuenta cuyo saldo puede leer esta sesión es la propia.
    const mia = await exigir('GET', `/cuentas/${estado.cuenta.id}`);
    const { cuentas } = await exigir('GET', '/cuentas?tipo=usuario&limite=50');
    const destino = cuentas.find((c) => c.id !== mia.id);
    if (!destino) { avisar('Hace falta otra cuenta de usuario para sembrarle', true); return; }

    const cantidad = 5;
    const monto = Math.min(10000, Math.floor(mia.saldo_centavos / cantidad));
    if (monto <= 0) {
      avisar('Tu cuenta no tiene saldo: cargá plata en Ingresar antes de sembrar', true);
      return;
    }

    const s = await exigir('POST', '/admin/semilla', {
      admin: true,
      cuerpo: {
        periodo: periodoActual(-1),
        origen_id: mia.id,
        destino_id: destino.id,
        cantidad,
        monto_centavos: monto,
      },
    });
    avisar(`${s.transferencias} transferencias de ${enPesos(monto)} `
      + `a ${destino.alias}, fechadas en ${s.periodo}`);
  });

  cuerpo.replaceChildren(
    el('p', { clase: 'ayuda', texto: `Período en curso: ${periodoActual()}` }),
    el('div', { style: 'height:.75rem' }),
    el('div', { clase: 'fila-botones' }, verificar, archivar, semilla),
    salida);
}

async function pintarExtracto({ cuerpo }) {
  const actual = periodoActual();
  const periodo = el('input', { type: 'month', value: actual });
  const desde = el('input', { type: 'month', value: actual });
  const hasta = el('input', { type: 'month', value: actual });
  const salidaExtracto = el('div');
  const salidaExportacion = el('div');

  const ver = accion('Ver extracto', async () => {
    const e = await exigir('GET', `/cuentas/${estado.cuenta.id}/movimientos?periodo=${encodeURIComponent(periodo.value)}`);
    salidaExtracto.replaceChildren(
      el('div', { style: 'height:.75rem' }),
      el('div', { clase: 'rejilla' },
        dato('Período', e.periodo, 'mono'),
        dato('Origen', e.origen),
        dato('Movimientos', String(e.movimientos?.length ?? 0)),
        e.objeto ? dato('Objeto', e.objeto, 'mono') : null));
  });

  const mostrarExportacion = (x) => {
    const consultar = accion('Consultar', async () => {
      mostrarExportacion(await exigir('GET', `/exportaciones/${x.id}`));
    });
    salidaExportacion.replaceChildren(
      el('div', { style: 'height:.75rem' }),
      el('div', { clase: 'rejilla' },
        dato('Exportación', `N.º ${x.id}`),
        dato('Estado', x.estado),
        dato('Filas', String(x.filas ?? '—')),
        dato('Períodos de la base', String(x.periodos_base ?? '—')),
        dato('Períodos del archivo', String(x.periodos_archivo ?? '—')),
        x.uri ? dato('Objeto', x.uri, 'mono') : null,
        x.motivo ? dato('Motivo', x.motivo) : null),
      el('div', { style: 'height:.75rem' }),
      el('div', { clase: 'fila-botones' }, consultar));
  };

  const exportar = accion('Exportar', async () => {
    mostrarExportacion(await exigir('POST', '/exportaciones', { cuerpo: { desde: desde.value, hasta: hasta.value } }));
  });

  cuerpo.replaceChildren(
    el('div', { clase: 'rejilla' },
      el('label', { clase: 'campo' }, el('span', { texto: 'Período' }), periodo)),
    el('div', { clase: 'fila-botones' }, ver),
    salidaExtracto,
    el('div', { style: 'height:1rem' }),
    el('div', { clase: 'rejilla' },
      el('label', { clase: 'campo' }, el('span', { texto: 'Desde' }), desde),
      el('label', { clase: 'campo' }, el('span', { texto: 'Hasta' }), hasta)),
    el('div', { clase: 'fila-botones' }, exportar),
    salidaExportacion);
}

// Hay que escribir BORRAR y no alcanza un clic: es irreversible y un confirm() nativo bloquea la
// página y no se puede automatizar.
async function pintarReinicio({ cuerpo }) {
  if (faltaClave(cuerpo, 'admin')) return;

  const salida = el('div');
  const palabra = el('input', { placeholder: 'BORRAR', autocomplete: 'off', 'aria-label': 'Escribí BORRAR para confirmar' });

  const ejecutar = accion('Borrar todos los datos', async () => {
    const res = await exigir('POST', '/admin/reinicio', {
      admin: true,
      cuerpo: { confirmar: 'BORRAR TODO' },
    });
    const total = Object.values(res.borradas).reduce((a, b) => a + b, 0);
    avisar(`${total} filas borradas. Las cuatro cuentas internas quedan.`);
    palabra.value = '';
    // accion() rehabilita el botón al terminar: se vuelve a bloquear hasta que escriban de nuevo.
    setTimeout(() => { ejecutar.disabled = true; }, 0);
    salida.replaceChildren(el('pre', { texto: JSON.stringify(res, null, 2) }));
  }, 'boton boton-peligro boton-chico');
  ejecutar.disabled = true;
  palabra.addEventListener('input', () => { ejecutar.disabled = palabra.value.trim() !== 'BORRAR'; });

  cuerpo.replaceChildren(
    el('p', { clase: 'ayuda', texto: 'Borra cuentas, órdenes, asientos, avisos y el estado del mock. El esquema y las cuatro cuentas internas del ledger quedan: sus ids están cacheados en el proceso y recrearlas lo rompería.' }),
    el('div', { style: 'height:.75rem' }),
    el('label', { clase: 'campo', style: 'max-width:18rem' },
      el('span', { clase: 'campo-rotulo', texto: 'Para confirmar, escribí BORRAR' }), palabra),
    el('div', { style: 'height:.75rem' }),
    el('div', { clase: 'fila-botones' }, ejecutar),
    salida);
}

function tarjetaCinta() {
  const lista = el('div', { clase: 'cinta' });

  const repintar = () => {
    const entradas = verCinta();
    lista.replaceChildren();
    if (!entradas.length) {
      lista.append(el('p', { clase: 'vacio', texto: 'Sin llamadas todavía' }));
      return;
    }
    for (const e of entradas) lista.append(filaCinta(e));
  };

  const desuscribir = alAnotar((entrada) => {
    if (!lista.isConnected) { desuscribir(); return; }
    if (!entrada) { repintar(); return; }
    lista.querySelector('.vacio')?.remove();
    lista.prepend(filaCinta(entrada));
    while (lista.children.length > verCinta().length) lista.lastChild.remove();
  });

  repintar();

  return el('section', { clase: 'tarjeta' },
    el('div', { clase: 'cabecera-tarjeta' },
      el('h2', { texto: 'Llamadas a la API' }),
      el('button', { clase: 'boton boton-chico', texto: 'Limpiar', onclick: limpiarCinta })),
    lista);
}

function filaCinta(e) {
  const clase = e.codigo === 0 ? 'codigo-5' : `codigo-${String(e.codigo)[0]}`;
  const detalle = el('pre', { hidden: true, texto: JSON.stringify(e.datos ?? e.error ?? null, null, 2) });

  const fila = el('div', {
    clase: 'cinta-fila',
    onclick: () => { detalle.hidden = !detalle.hidden; },
  },
    el('span', { clase: `codigo ${clase}`, texto: e.codigo === 0 ? 'ERR' : String(e.codigo) }),
    el('span', { clase: 'ruta', texto: `${e.metodo} ${e.ruta}` }),
    el('span', { clase: 'ms', texto: `${e.ms} ms` }));

  return el('div', {}, fila, detalle);
}
