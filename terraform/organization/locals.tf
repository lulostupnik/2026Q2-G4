locals {
  name_prefix = var.project_name
  api_url     = "http://${module.load_balancer.dns_name}"

  common_tags = {
    Project   = "nucleo-ledger"
    TP        = "tp3"
    ManagedBy = "terraform"
  }

  az_count = 2
  azs      = slice(data.aws_availability_zones.available.names, 0, local.az_count)

  subnet_offsets = {
    public  = 0
    app     = 10
    archive = 12
    data    = 20
  }

  subnet_cidrs = {
    for layer, offset in local.subnet_offsets :
    layer => [for i in range(local.az_count) : cidrsubnet(var.vpc_cidr, 8, offset + i)]
  }

  subnet_names = {
    for layer in keys(local.subnet_offsets) :
    layer => [for az in local.azs : "${local.name_prefix}-${layer}-${substr(az, -1, 1)}"]
  }

  services = {
    api = {
      command           = ["/api"]
      instance_type     = var.api_instance_type
      target_capacity   = 80
      cpu               = 1024
      memory            = 512
      port              = var.app_port
      target_group_arn  = module.load_balancer.target_group_arn
      subnet_ids        = module.vpc.private_subnets
      security_group_id = module.security_groups.ids.app
      min_count         = 2
      max_count         = 5 # el ASG admite max_count + 1, y AWS Academy no deja más de 6
      cpu_target        = 60
      schedule          = {}
      environment = {
        APP_PORT          = tostring(var.app_port)
        S3_BUCKET         = module.storage.bucket_name
        AWS_REGION        = var.aws_region
        COGNITO_ISSUER    = module.auth.issuer
        COGNITO_CLIENT_ID = module.auth.client_id
        CORS_ORIGENES     = module.website.website_url
        PROVEEDOR_URL     = coalesce(var.proveedor_url, "${local.api_url}/servicio-mock")
        WEBHOOK_URL       = var.webhook_url
      }
    }
    archive = {
      command           = ["/archivador"]
      instance_type     = var.archive_instance_type
      target_capacity   = 100
      cpu               = 512
      memory            = 256
      port              = null
      target_group_arn  = null
      subnet_ids        = module.vpc.intra_subnets
      security_group_id = module.security_groups.ids.archive
      min_count         = 0
      max_count         = 1
      cpu_target        = null
      schedule          = var.archive_schedule
      environment = {
        S3_BUCKET  = module.storage.bucket_name
        AWS_REGION = var.aws_region
      }
    }
  }

  # Cota para un deploy: los dos servicios a la vez en el doble de max_count
  # (deployment_maximum_percent = 200).
  db_pool_size = floor(var.db_max_connections / sum([for s in values(local.services) : 2 * s.max_count]))
  db_url       = "${module.database.url}&pool_max_conns=${local.db_pool_size}"

  # Aparte de services: son sensibles, y for_each no acepta valores sensibles.
  mock_api_key = coalesce(var.mock_api_key, random_password.this["mock"].result)

  secret_values = {
    api = {
      DB_URL            = local.db_url
      ADMIN_API_KEY     = random_password.this["admin"].result
      MOCK_API_KEY      = local.mock_api_key
      PROVEEDOR_API_KEY = coalesce(var.proveedor_api_key, local.mock_api_key)
      WEBHOOK_SECRET    = coalesce(var.webhook_secret, random_password.this["webhook"].result)
    }
    archive = {
      DB_URL = local.db_url
    }
  }
}
