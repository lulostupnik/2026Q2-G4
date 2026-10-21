resource "random_password" "this" {
  for_each = toset(["db", "admin", "mock", "webhook"])

  length  = 32
  special = false
}

resource "aws_secretsmanager_secret" "this" {
  for_each = local.services

  name        = "${local.name_prefix}/${each.key}"
  description = "Variables secretas de ${local.name_prefix}-${each.key}"
  kms_key_id  = aws_kms_key.this.arn

  # Sin ventana de recuperación: después de un destroy el nombre queda libre para el próximo deploy.
  recovery_window_in_days = 0
}

resource "aws_secretsmanager_secret_version" "this" {
  for_each = local.services

  secret_id     = aws_secretsmanager_secret.this[each.key].id
  secret_string = jsonencode(local.secret_values[each.key])
}
