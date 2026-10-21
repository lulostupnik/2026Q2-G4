module "security_groups" {
  source = "../modules/security_groups"

  name_prefix       = local.name_prefix
  vpc_id            = module.vpc.vpc_id
  app_port          = var.app_port
  db_port           = var.db_port
  s3_prefix_list_id = aws_vpc_endpoint.s3.prefix_list_id
}

module "storage" {
  source = "../modules/storage"

  name_prefix = local.name_prefix
  kms_key_arn = aws_kms_key.this.arn
}

module "website" {
  source = "../modules/website"

  name_prefix = "${local.name_prefix}-web"
  site_dir    = "${path.root}/../../apps/web"

  runtime_config = {
    api_url    = local.api_url
    login_url  = module.auth.login_url
    logout_url = module.auth.logout_url
  }
}

module "auth" {
  source = "../modules/auth"

  name_prefix         = local.name_prefix
  frontend_url        = module.website.website_url
  lambda_role_arn     = data.aws_iam_role.lab.arn
  callback_source_dir = "${path.root}/../../apps/callback"
  kms_key_arn         = aws_kms_key.this.arn
}

module "database" {
  source = "../modules/database"

  name_prefix          = local.name_prefix
  db_subnet_group_name = module.vpc.database_subnet_group_name
  security_group_id    = module.security_groups.ids.data
  password             = random_password.this["db"].result
  port                 = var.db_port
  engine_version       = var.db_engine_version
  instance_class       = var.db_instance_class
  multi_az             = var.db_multi_az
  kms_key_arn          = aws_kms_key.this.arn
}

module "load_balancer" {
  source = "../modules/load_balancer"

  name_prefix       = local.name_prefix
  vpc_id            = module.vpc.vpc_id
  public_subnet_ids = module.vpc.public_subnets
  security_group_id = module.security_groups.ids.nlb
  app_port          = var.app_port
  health_check_path = "/health"
}

module "capacity" {
  source   = "../modules/capacity"
  for_each = local.services

  name                 = "${local.name_prefix}-${each.key}"
  cluster_name         = aws_ecs_cluster.this.name
  subnet_ids           = each.value.subnet_ids
  instance_type        = each.value.instance_type
  instance_profile_arn = data.aws_iam_instance_profile.lab.arn
  security_group_id    = module.security_groups.ids.host
  max_size             = each.value.max_count + 1
  target_capacity      = each.value.target_capacity

  depends_on = [aws_vpc_endpoint.interface, module.security_groups]
}

module "service" {
  source   = "../modules/service"
  for_each = local.services

  name = "${local.name_prefix}-${each.key}"
  # De la asociación y no del cluster: el servicio necesita su capacity provider ya asociado.
  cluster_name           = aws_ecs_cluster_capacity_providers.this.cluster_name
  capacity_provider_name = module.capacity[each.key].capacity_provider_name
  image                  = "${aws_ecr_repository.ledger.repository_url}:${var.image_tag}"
  command                = each.value.command
  cpu                    = each.value.cpu
  memory                 = each.value.memory
  environment            = each.value.environment
  # Con el version_id, un valor nuevo cambia la task definition y ECS reemplaza las tareas.
  secrets = {
    for nombre in nonsensitive(keys(local.secret_values[each.key])) :
    nombre => "${aws_secretsmanager_secret.this[each.key].arn}:${nombre}::${aws_secretsmanager_secret_version.this[each.key].version_id}"
  }
  log_kms_key_arn   = aws_kms_key.this.arn
  port              = each.value.port
  target_group_arn  = each.value.target_group_arn
  subnet_ids        = each.value.subnet_ids
  security_group_id = each.value.security_group_id
  role_arn          = data.aws_iam_role.lab.arn
  min_count         = each.value.min_count
  max_count         = each.value.max_count
  cpu_target        = each.value.cpu_target
  schedule          = each.value.schedule
}
