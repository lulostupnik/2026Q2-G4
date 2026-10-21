import { aleatorio } from './api.js';

const LLAVE_STATE = 'nucleo.state';

const ERRORES = {
  access_denied: 'Se canceló el inicio de sesión',
  missing_code: 'Cognito volvió sin el código de autorización',
  token_exchange_failed: 'No se pudieron obtener los tokens de Cognito',
};

export async function leerConfig() {
  let r;
  try {
    r = await fetch('config.json', { cache: 'no-store' });
  } catch {
    throw new Error('No se pudo leer config.json');
  }
  if (!r.ok) throw new Error(`No se pudo leer config.json (${r.status})`);
  const config = await r.json().catch(() => null);
  const faltan = ['api_url', 'login_url', 'logout_url'].filter((k) => !config?.[k]);
  if (faltan.length) throw new Error(`A config.json le falta ${faltan.join(', ')}`);
  return config;
}

// sessionStorage y no localStorage: el state vale para esta pestaña y para un solo login.
function guardarState(valor) {
  try { sessionStorage.setItem(LLAVE_STATE, valor); } catch { /* sin persistencia */ }
}
function tomarState() {
  try {
    const valor = sessionStorage.getItem(LLAVE_STATE);
    sessionStorage.removeItem(LLAVE_STATE);
    return valor || '';
  } catch { return ''; }
}

export function irAlLogin(config) {
  const state = aleatorio(24);
  guardarState(state);
  location.assign(`${config.login_url}&state=${encodeURIComponent(state)}`);
}

export function irAlLogout(config) {
  location.assign(config.logout_url);
}

// La Lambda del callback vuelve a <frontend>/#access_token=…&state=… o a #error=…&state=….
export function leerVuelta() {
  const fragmento = new URLSearchParams(location.hash.slice(1));
  if (!fragmento.has('state') || !(fragmento.has('access_token') || fragmento.has('error'))) return null;

  history.replaceState(null, '', location.pathname + location.search);

  const esperado = tomarState();
  if (!esperado || fragmento.get('state') !== esperado) {
    return { error: 'La respuesta del inicio de sesión no corresponde a este navegador. Volvé a ingresar.' };
  }
  if (fragmento.has('error')) {
    const codigo = fragmento.get('error');
    return { error: ERRORES[codigo] || `Cognito respondió ${codigo}` };
  }
  return {
    tokens: {
      accessToken: fragmento.get('access_token'),
      idToken: fragmento.get('id_token') || '',
      expiraEnSegundos: Number(fragmento.get('expires_in')) || 0,
    },
  };
}
