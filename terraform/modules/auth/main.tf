data "aws_caller_identity" "current" {}
data "aws_region" "current" {}

locals {
  domain_url   = format("https://%s.auth.%s.amazoncognito.com", aws_cognito_user_pool_domain.this.domain, data.aws_region.current.region)
  callback_url = "${aws_apigatewayv2_api.this.api_endpoint}/callback"
  logout_url   = "${aws_apigatewayv2_api.this.api_endpoint}/logout"
}

# --- Cognito ---------------------------------------------------------------

resource "aws_cognito_user_pool" "this" {
  name = "${var.name_prefix}-users"

  username_attributes      = ["email"]
  auto_verified_attributes = ["email"]
  deletion_protection      = "INACTIVE"

  admin_create_user_config {
    allow_admin_create_user_only = !var.self_sign_up
  }

  password_policy {
    minimum_length                   = 8
    require_lowercase                = true
    require_numbers                  = true
    require_symbols                  = false
    require_uppercase                = false
    temporary_password_validity_days = 7
  }

  account_recovery_setting {
    recovery_mechanism {
      name     = "verified_email"
      priority = 1
    }
  }
}

resource "aws_cognito_user_pool_domain" "this" {
  domain                = "${var.name_prefix}-${data.aws_caller_identity.current.account_id}"
  user_pool_id          = aws_cognito_user_pool.this.id
  managed_login_version = 1 # Hosted UI clásica: la v2 exige además un recurso de branding
}

resource "aws_cognito_user_pool_client" "this" {
  name         = "${var.name_prefix}-web"
  user_pool_id = aws_cognito_user_pool.this.id

  generate_secret                      = true
  allowed_oauth_flows_user_pool_client = true
  allowed_oauth_flows                  = ["code"]
  allowed_oauth_scopes                 = ["openid", "email"]
  supported_identity_providers         = ["COGNITO"]
  explicit_auth_flows                  = ["ALLOW_USER_SRP_AUTH", "ALLOW_REFRESH_TOKEN_AUTH"]
  prevent_user_existence_errors        = "ENABLED"

  callback_urls = [local.callback_url]
  logout_urls   = [local.logout_url]

  access_token_validity = var.token_validity_hours
  id_token_validity     = var.token_validity_hours

  token_validity_units {
    access_token  = "hours"
    id_token      = "hours"
    refresh_token = "days"
  }
}

resource "aws_secretsmanager_secret" "client" {
  name                    = "${var.name_prefix}/callback"
  description             = "Client secret del app client de Cognito, para la Lambda del callback"
  kms_key_id              = var.kms_key_arn
  recovery_window_in_days = 0
}

resource "aws_secretsmanager_secret_version" "client" {
  secret_id     = aws_secretsmanager_secret.client.id
  secret_string = aws_cognito_user_pool_client.this.client_secret
}

data "archive_file" "callback" {
  type        = "zip"
  source_dir  = var.callback_source_dir
  output_path = "${path.root}/.build/${var.name_prefix}-callback.zip"
}

resource "aws_cloudwatch_log_group" "callback" {
  name              = "/aws/lambda/${var.name_prefix}-callback"
  retention_in_days = var.log_retention_days
  kms_key_id        = var.kms_key_arn
}

# Sin X-Ray: en AWS Academy las trazas no se pueden consultar (el usuario del laboratorio no tiene
# permisos de lectura de X-Ray).
#tfsec:ignore:aws-lambda-enable-tracing
resource "aws_lambda_function" "callback" {
  function_name    = "${var.name_prefix}-callback"
  description      = "Canjea el code de Cognito por tokens y redirige al frontend HTTP"
  role             = var.lambda_role_arn
  runtime          = var.lambda_runtime
  handler          = "handler.handler"
  filename         = data.archive_file.callback.output_path
  source_code_hash = data.archive_file.callback.output_base64sha256
  timeout          = 10

  environment {
    variables = {
      COGNITO_DOMAIN_URL = local.domain_url
      CLIENT_ID          = aws_cognito_user_pool_client.this.id
      CLIENT_SECRET_ARN  = aws_secretsmanager_secret.client.arn
      CALLBACK_URL       = local.callback_url
      FRONTEND_URL       = var.frontend_url
    }
  }

  depends_on = [aws_cloudwatch_log_group.callback]
}

resource "aws_apigatewayv2_api" "this" {
  name          = "${var.name_prefix}-auth"
  protocol_type = "HTTP"
  description   = "Callback y logout HTTPS de Cognito"
}

resource "aws_apigatewayv2_integration" "callback" {
  api_id                 = aws_apigatewayv2_api.this.id
  integration_type       = "AWS_PROXY"
  integration_uri        = aws_lambda_function.callback.invoke_arn
  payload_format_version = "2.0"
}

resource "aws_apigatewayv2_route" "this" {
  for_each = toset(["GET /callback", "GET /logout"])

  api_id    = aws_apigatewayv2_api.this.id
  route_key = each.key
  target    = "integrations/${aws_apigatewayv2_integration.callback.id}"
}

resource "aws_cloudwatch_log_group" "api" {
  name              = "/aws/apigateway/${var.name_prefix}-auth"
  retention_in_days = var.log_retention_days
  kms_key_id        = var.kms_key_arn
}

resource "aws_apigatewayv2_stage" "default" {
  api_id      = aws_apigatewayv2_api.this.id
  name        = "$default"
  auto_deploy = true

  access_log_settings {
    destination_arn = aws_cloudwatch_log_group.api.arn
    format = jsonencode({
      requestId        = "$context.requestId"
      ip               = "$context.identity.sourceIp"
      requestTime      = "$context.requestTime"
      routeKey         = "$context.routeKey"
      status           = "$context.status"
      latency          = "$context.responseLatency"
      integrationError = "$context.integrationErrorMessage"
    })
  }
}

resource "aws_lambda_permission" "api_gateway" {
  statement_id  = "AllowAPIGatewayInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.callback.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.this.execution_arn}/*/*"
}
