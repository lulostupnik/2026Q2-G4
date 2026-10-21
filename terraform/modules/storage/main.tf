data "aws_caller_identity" "current" {}
data "aws_region" "current" {}

# El bucket no es un aws_s3_bucket: lo crea scripts/buckets.sh (el SCP de AWS Academy no deja
# leer su object lock, y el provider lo lee en cada refresh). Acá sólo se configura.
locals {
  bucket_name = format("%s-%s-%s-an", var.name_prefix, data.aws_caller_identity.current.account_id, data.aws_region.current.region)
  bucket_arn  = "arn:aws:s3:::${local.bucket_name}"
}

resource "aws_s3_bucket_public_access_block" "this" {
  bucket = local.bucket_name

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_policy" "this" {
  bucket = local.bucket_name

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Sid       = "DenyInsecureTransport"
      Effect    = "Deny"
      Principal = "*"
      Action    = "s3:*"
      Resource  = [local.bucket_arn, "${local.bucket_arn}/*"]
      Condition = { Bool = { "aws:SecureTransport" = "false" } }
    }]
  })

  depends_on = [aws_s3_bucket_public_access_block.this]
}

resource "aws_s3_bucket_server_side_encryption_configuration" "this" {
  bucket = local.bucket_name
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm     = "aws:kms"
      kms_master_key_id = var.kms_key_arn
    }
    # Una clave de datos por bucket y no por objeto: menos llamadas a KMS, que se cobran.
    bucket_key_enabled = true
  }
}

resource "aws_s3_bucket_versioning" "this" {
  bucket = local.bucket_name

  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_lifecycle_configuration" "this" {
  bucket = local.bucket_name

  rule {
    id     = "noncurrent-versions"
    status = "Enabled"

    filter {}

    noncurrent_version_expiration {
      noncurrent_days = var.noncurrent_version_days
    }
  }

  dynamic "rule" {
    for_each = var.transitions

    content {
      id     = rule.key
      status = "Enabled"

      filter {
        prefix = rule.value.prefix
      }

      dynamic "transition" {
        for_each = rule.value.transitions

        content {
          days          = transition.value.days
          storage_class = transition.value.storage_class
        }
      }
    }
  }

  depends_on = [aws_s3_bucket_versioning.this]
}

