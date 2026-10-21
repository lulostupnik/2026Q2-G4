// Íconos de trazo, dibujados acá y no con emojis: se ven igual en todos los sistemas y toman el
// color del texto (currentColor), así que respetan el modo oscuro.
const TRAZOS = {
  inicio: ['M3 10.5 12 3l9 7.5', 'M5 9.5V20h14V9.5', 'M10 20v-6h4v6'],
  enviar: ['M7 17 17 7', 'M8 7h9v9'],
  ingresar: ['M17 7 7 17', 'M16 17H7V8'],
  retirar: ['M12 3 3 8h18z', 'M3 10h18', 'M5 10v8', 'M9.5 10v8', 'M14.5 10v8', 'M19 10v8', 'M3 20h18'],
  actividad: ['M8 6h13', 'M8 12h13', 'M8 18h13', 'M3.5 6h.01', 'M3.5 12h.01', 'M3.5 18h.01'],
  operacion: ['M4 21v-7', 'M4 10V3', 'M12 21v-9', 'M12 8V3', 'M20 21v-5', 'M20 12V3', 'M1 14h6', 'M9 8h6', 'M17 16h6'],
  salir: ['M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4', 'M16 17l5-5-5-5', 'M21 12H9'],
  ojo: ['M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z', 'M12 9a3 3 0 1 0 0 6 3 3 0 0 0 0-6z'],
  ojoTachado: ['M3 3l18 18', 'M10.6 5.1A10.9 10.9 0 0 1 12 5c6.5 0 10 7 10 7a17.6 17.6 0 0 1-3.2 4.2',
    'M6.6 6.6C3.9 8.4 2 12 2 12s3.5 7 10 7a9.7 9.7 0 0 0 5.4-1.6', 'M9.9 9.9a3 3 0 0 0 4.2 4.2'],
  copiar: ['M9 9h11v11H9z', 'M5 15H4a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1h10a1 1 0 0 1 1 1v1'],
  ok: ['M20 6 9 17l-5-5'],
  cruz: ['M18 6 6 18', 'M6 6l12 12'],
  reloj: ['M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18z', 'M12 7v5l3 2'],
  buscar: ['M11 4a7 7 0 1 0 0 14 7 7 0 0 0 0-14z', 'M21 21l-4.35-4.35'],
  atras: ['M19 12H5', 'M12 19l-7-7 7-7'],
  izquierda: ['M15 18l-6-6 6-6'],
  derecha: ['M9 18l6-6-6-6'],
  servidor: ['M4 4h16v6H4z', 'M4 14h16v6H4z', 'M8 7h.01', 'M8 17h.01'],
  llave: ['M16 4a4 4 0 1 0 0 8 4 4 0 0 0 0-8z', 'M13 11 3 21', 'M6 18l2 2', 'M9 15l2 2'],
  escudo: ['M12 3 4 6v6c0 5 3.5 8 8 9 4.5-1 8-4 8-9V6z', 'M9 12l2 2 4-4'],
  alerta: ['M12 3 2 20h20z', 'M12 10v4', 'M12 17h.01'],
  info: ['M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18z', 'M12 11v5', 'M12 8h.01'],
  usuario: ['M12 12a4 4 0 1 0 0-8 4 4 0 0 0 0 8z', 'M4 21a8 8 0 0 1 16 0'],
  billetera: ['M3 7a2 2 0 0 1 2-2h13v4', 'M3 7v11a2 2 0 0 0 2 2h15V9H5a2 2 0 0 1-2-2z', 'M16 14h.01'],
  recargar: ['M20 11a8 8 0 0 0-14.9-3.9L3 9', 'M3 4v5h5', 'M4 13a8 8 0 0 0 14.9 3.9L21 15', 'M21 20v-5h-5'],
  basura: ['M3 6h18', 'M8 6V4h8v2', 'M6 6l1 14h10l1-14'],
  mas: ['M12 5v14', 'M5 12h14'],
  rayo: ['M13 2 4 14h7l-1 8 9-12h-7z'],
  bajar: ['M12 3v12', 'M7 10l5 5 5-5', 'M4 21h16'],
  calendario: ['M4 5h16v16H4z', 'M4 10h16', 'M9 3v4', 'M15 3v4'],
};

const SVG = 'http://www.w3.org/2000/svg';

export function icono(nombre, tam = 20) {
  const svg = document.createElementNS(SVG, 'svg');
  svg.setAttribute('viewBox', '0 0 24 24');
  svg.setAttribute('width', tam);
  svg.setAttribute('height', tam);
  svg.setAttribute('fill', 'none');
  svg.setAttribute('stroke', 'currentColor');
  svg.setAttribute('stroke-width', '2');
  svg.setAttribute('stroke-linecap', 'round');
  svg.setAttribute('stroke-linejoin', 'round');
  svg.setAttribute('aria-hidden', 'true');
  svg.classList.add('ico');
  for (const d of TRAZOS[nombre] || TRAZOS.info) {
    const p = document.createElementNS(SVG, 'path');
    p.setAttribute('d', d);
    svg.append(p);
  }
  return svg;
}
