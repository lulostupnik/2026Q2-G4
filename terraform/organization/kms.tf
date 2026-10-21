resource "aws_kms_key" "this" {
  description             = "${local.name_prefix}: secretos, archivo del ledger, RDS, ECR y logs"
  enable_key_rotation     = true
  deletion_window_in_days = 7 # el mínimo: después de un destroy la clave no queda un mes en la cuenta

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        # Sin esta, nadie podría administrar la clave: delega en las políticas de IAM de la cuenta.
        Sid       = "Cuenta"
        Effect    = "Allow"
        Principal = { AWS = "arn:aws:iam::${data.aws_caller_identity.current.account_id}:root" }
        Action    = "kms:*"
        Resource  = "*"
      },
      {
        # Explícito en la clave: no depende de que la política del LabRole incluya KMS.
        Sid       = "LabRole"
        Effect    = "Allow"
        Principal = { AWS = data.aws_iam_role.lab.arn }
        Action    = ["kms:Decrypt", "kms:Encrypt", "kms:GenerateDataKey*", "kms:ReEncrypt*", "kms:DescribeKey"]
        Resource  = "*"
      },
      {
        Sid       = "CloudWatchLogs"
        Effect    = "Allow"
        Principal = { Service = "logs.${var.aws_region}.amazonaws.com" }
        Action    = ["kms:Decrypt*", "kms:Encrypt*", "kms:GenerateDataKey*", "kms:ReEncrypt*", "kms:Describe*"]
        Resource  = "*"
        Condition = {
          ArnLike = {
            "kms:EncryptionContext:aws:logs:arn" = "arn:aws:logs:${var.aws_region}:${data.aws_caller_identity.current.account_id}:log-group:*"
          }
        }
      },
    ]
  })
}

resource "aws_kms_alias" "this" {
  name          = "alias/${local.name_prefix}"
  target_key_id = aws_kms_key.this.key_id
}
