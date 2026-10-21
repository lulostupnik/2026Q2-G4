output "bucket_name" {
  description = "Nombre del bucket del frontend."
  value       = local.bucket_name
}

output "website_url" {
  description = "URL HTTP del frontend (destino del redirect de la Lambda de Cognito)."
  value       = "http://${aws_s3_bucket_website_configuration.this.website_endpoint}"
}
