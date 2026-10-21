// Callback HTTPS de Cognito (workaround del redirect).

import { GetSecretValueCommand, SecretsManagerClient } from "@aws-sdk/client-secrets-manager";

const { COGNITO_DOMAIN_URL, CLIENT_ID, CLIENT_SECRET_ARN, CALLBACK_URL } = process.env;
const FRONTEND_URL = process.env.FRONTEND_URL.replace(/\/$/, "");

const secretsManager = new SecretsManagerClient({});
let clientSecret;

// Una lectura por instancia de la Lambda. Si falla no queda guardado: se reintenta en el próximo login.
const leerClientSecret = async () => {
  clientSecret ??= (await secretsManager.send(new GetSecretValueCommand({ SecretId: CLIENT_SECRET_ARN }))).SecretString;
  return clientSecret;
};

export const handler = async (event) => {
  if (event.rawPath === "/logout") {
    return redirect(`${FRONTEND_URL}/`);
  }

  const params = event.queryStringParameters ?? {};
  const state = params.state ?? "";

  if (params.error) {
    return back({ error: params.error, state });
  }
  if (!params.code) {
    return back({ error: "missing_code", state });
  }

  let secret;
  try {
    secret = await leerClientSecret();
  } catch {
    return back({ error: "token_exchange_failed", state });
  }

  // Un timeout o un corte de red también vuelven al frontend con el error, no en un 500 de API Gateway.
  let tokens;
  try {
    const res = await fetch(`${COGNITO_DOMAIN_URL}/oauth2/token`, {
      method: "POST",
      headers: {
        "Content-Type": "application/x-www-form-urlencoded",
        Authorization: `Basic ${Buffer.from(`${CLIENT_ID}:${secret}`).toString("base64")}`,
      },
      body: new URLSearchParams({
        grant_type: "authorization_code",
        client_id: CLIENT_ID,
        code: params.code,
        redirect_uri: CALLBACK_URL,
      }),
      signal: AbortSignal.timeout(5000),
    });
    tokens = res.ok ? await res.json() : null;
  } catch {
    tokens = null;
  }

  if (!tokens?.access_token) {
    return back({ error: "token_exchange_failed", state });
  }

  return back({
    access_token: tokens.access_token,
    id_token: tokens.id_token,
    expires_in: tokens.expires_in,
    token_type: tokens.token_type,
    state,
  });
};

// Sin los campos que no vinieron: URLSearchParams escribiría "undefined" como valor.
const back = (fragment) => redirect(`${FRONTEND_URL}/#${new URLSearchParams(
  Object.entries(fragment).filter(([, valor]) => valor !== undefined && valor !== null),
)}`);

const redirect = (location) => ({
  statusCode: 302,
  headers: { Location: location, "Cache-Control": "no-store" },
});
