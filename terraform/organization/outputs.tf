output "vpc_id" {
  description = "ID de la VPC."
  value       = module.vpc.vpc_id
}

output "subnet_ids" {
  description = "Subredes por capa."
  value = {
    public  = module.vpc.public_subnets
    app     = module.vpc.private_subnets
    archive = module.vpc.intra_subnets
    data    = module.vpc.database_subnets
  }
}

output "nat_public_ip" {
  description = "IP de salida fija hacia el proveedor (la que va a su lista blanca)."
  value       = module.vpc.nat_public_ips[0]
}

output "bucket_name" {
  description = "Bucket del proyecto: archivo del ledger."
  value       = module.storage.bucket_name
}

output "website_url" {
  description = "URL del frontend servido desde S3."
  value       = module.website.website_url
}

output "db_endpoint" {
  description = "Hostname de RDS. Sólo se alcanza desde la API y el archivador."
  value       = module.database.address
}

output "api_url" {
  description = "URL de la API, detrás del NLB. Probar con: curl <api_url>/health"
  value       = local.api_url
}

output "cognito" {
  description = "Login con Cognito: Hosted UI, callback HTTPS y lo que la API necesita para validar tokens."
  value = {
    login_url    = module.auth.login_url
    callback_url = module.auth.callback_url
    user_pool_id = module.auth.user_pool_id
    client_id    = module.auth.client_id
    issuer       = module.auth.issuer
  }
}

output "ecr_repository_url" {
  description = "Repositorio de la imagen del backend. make imagen sube ahí el commit actual."
  value       = aws_ecr_repository.ledger.repository_url
}

output "ecs_cluster_name" {
  description = "Cluster de ECS de la API y el archivador."
  value       = aws_ecs_cluster.this.name
}
