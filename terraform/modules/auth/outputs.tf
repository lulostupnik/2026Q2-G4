output "user_pool_id" {
  description = "ID del user pool. Para crear usuarios por CLI (admin-create-user)."
  value       = aws_cognito_user_pool.this.id
}

output "client_id" {
  description = "App client. La API lo compara con el claim client_id del access token."
  value       = aws_cognito_user_pool_client.this.id
}

output "issuer" {
  description = "Issuer de los tokens. La API valida contra <issuer>/.well-known/jwks.json."
  value       = "https://cognito-idp.${data.aws_region.current.region}.amazonaws.com/${aws_cognito_user_pool.this.id}"
}

output "callback_url" {
  description = "Callback HTTPS registrado en Cognito (API Gateway → Lambda)."
  value       = local.callback_url
}

output "login_url" {
  description = "Hosted UI lista para usar. El frontend le agrega &state=<random> antes de redirigir."
  value = format(
    "%s/oauth2/authorize?%s",
    local.domain_url,
    join("&", [
      "client_id=${aws_cognito_user_pool_client.this.id}",
      "response_type=code",
      "scope=${join("+", aws_cognito_user_pool_client.this.allowed_oauth_scopes)}",
      "redirect_uri=${urlencode(local.callback_url)}",
    ]),
  )
}

output "logout_url" {
  description = "Cierra la sesión de la Hosted UI y vuelve al frontend pasando por /logout."
  value       = "${local.domain_url}/logout?client_id=${aws_cognito_user_pool_client.this.id}&logout_uri=${urlencode(local.logout_url)}"
}
