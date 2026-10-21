locals {
  # El identifier de RDS acepta guiones, pero db_name y username sólo letras, dígitos y "_":
  # "ledger-prod" → base "ledger_prod", usuario "ledger_prod_admin".
  db_name = replace(var.name_prefix, "-", "_")
}

# Sin autenticación IAM: la API entra con la credencial de Secrets Manager; activarla sólo abriría
# una segunda vía de login que nadie usa. Sin Performance Insights: en AWS Academy sus métricas no
# se pueden consultar (el usuario del laboratorio no tiene permisos de lectura de pi:*).
#tfsec:ignore:aws-rds-enable-iam-auth
#tfsec:ignore:aws-rds-enable-performance-insights
resource "aws_db_instance" "this" {
  identifier = "${var.name_prefix}-db"

  engine                     = "postgres"
  engine_version             = var.engine_version
  auto_minor_version_upgrade = true
  instance_class             = var.instance_class

  allocated_storage = 20
  storage_type      = "gp3"
  storage_encrypted = true
  kms_key_id        = var.kms_key_arn

  db_name  = local.db_name
  username = "${local.db_name}_admin"
  password = var.password
  port     = var.port

  multi_az               = var.multi_az
  db_subnet_group_name   = var.db_subnet_group_name
  vpc_security_group_ids = [var.security_group_id]
  publicly_accessible    = false

  backup_retention_period = 7
  copy_tags_to_snapshot   = true

  deletion_protection = false #tfsec:ignore:aws-rds-enable-deletion-protection Destroy tiene que poder borrar la base
  skip_final_snapshot = true

  tags = {
    Name = "${var.name_prefix}-db"
  }
}

