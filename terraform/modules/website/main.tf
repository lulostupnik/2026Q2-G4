data "aws_caller_identity" "current" {}
data "aws_region" "current" {}

# El bucket no es un aws_s3_bucket: lo crea scripts/buckets.sh (el SCP de AWS Academy no deja
# leer su object lock, y el provider lo lee en cada refresh). Acá sólo se configura.
locals {
  bucket_name = format("%s-%s-%s-an", var.name_prefix, data.aws_caller_identity.current.account_id, data.aws_region.current.region)
  bucket_arn  = "arn:aws:s3:::${local.bucket_name}"

  site_files = {
    for path in fileset(var.site_dir, "**") :
    path => var.content_types[lower(regex("[^.]*$", path))]
    if contains(keys(var.content_types), lower(regex("[^.]*$", path))) && path != "config.json"
  }
}

resource "aws_s3_bucket_website_configuration" "this" {
  bucket = local.bucket_name

  index_document {
    suffix = var.index_document
  }
}

resource "aws_s3_bucket_public_access_block" "this" {
  bucket = local.bucket_name

  block_public_acls       = true
  ignore_public_acls      = true
  block_public_policy     = false #tfsec:ignore:aws-s3-block-public-policy
  restrict_public_buckets = false #tfsec:ignore:aws-s3-no-public-buckets
}

# AES256 y no KMS: los usuarios anónimos del sitio no pueden tener permiso sobre una clave.
#tfsec:ignore:aws-s3-encryption-customer-key
resource "aws_s3_bucket_server_side_encryption_configuration" "this" {
  bucket = local.bucket_name

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_policy" "public_read" {
  bucket = local.bucket_name

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Sid       = "PublicRead"
      Effect    = "Allow"
      Principal = "*"
      Action    = "s3:GetObject"
      Resource  = "${local.bucket_arn}/*"
    }]
  })

  depends_on = [aws_s3_bucket_public_access_block.this]
}

resource "aws_s3_object" "site" {
  for_each = local.site_files

  bucket       = local.bucket_name
  key          = each.key
  source       = "${var.site_dir}/${each.key}"
  source_hash  = filemd5("${var.site_dir}/${each.key}")
  content_type = each.value
}

# Lo que el frontend no puede saber hasta el apply (URL de la API, Hosted UI, ...). Se genera
# acá y no en site_dir: si existiera también allá, dos recursos pisarían el mismo objeto.
resource "aws_s3_object" "config" {
  count = length(var.runtime_config) > 0 ? 1 : 0

  bucket        = local.bucket_name
  key           = "config.json"
  content       = jsonencode(var.runtime_config)
  content_type  = var.content_types["json"]
  cache_control = "no-cache"
}
