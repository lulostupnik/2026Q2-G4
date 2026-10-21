# Sin Flow Logs: a CloudWatch Logs no pueden entregar (la trust policy de LabRole no incluye
# vpc-flow-logs.amazonaws.com), y a S3 habría que sumar un bucket propio o abrirle a
# delivery.logs.amazonaws.com la política y la clave KMS del bucket de datos.
#tfsec:ignore:aws-ec2-require-vpc-flow-logs-for-all-vpcs
module "vpc" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "~> 6.7"

  name = "${local.name_prefix}-vpc"
  cidr = var.vpc_cidr
  azs  = local.azs

  public_subnets   = local.subnet_cidrs.public
  private_subnets  = local.subnet_cidrs.app
  intra_subnets    = local.subnet_cidrs.archive
  database_subnets = local.subnet_cidrs.data

  public_subnet_names   = local.subnet_names.public
  private_subnet_names  = local.subnet_names.app
  intra_subnet_names    = local.subnet_names.archive
  database_subnet_names = local.subnet_names.data

  public_route_table_tags   = { Name = "${local.name_prefix}-rtb-public" }
  private_route_table_tags  = { Name = "${local.name_prefix}-rtb-app" }
  intra_route_table_tags    = { Name = "${local.name_prefix}-rtb-archive" }
  database_route_table_tags = { Name = "${local.name_prefix}-rtb-data" }

  igw_tags                   = { Name = "${local.name_prefix}-igw" }
  nat_gateway_tags           = { Name = "${local.name_prefix}-nat" }
  nat_eip_tags               = { Name = "${local.name_prefix}-nat-eip" }
  database_subnet_group_name = "${local.name_prefix}-db-subnets"

  enable_nat_gateway = true
  single_nat_gateway = true

  create_database_subnet_route_table     = true
  create_database_nat_gateway_route      = false
  create_database_internet_gateway_route = false
  create_database_subnet_group           = true

  enable_dns_hostnames = true
  enable_dns_support   = true

  default_route_table_name = "${local.name_prefix}-rtb-default"
  default_network_acl_name = "${local.name_prefix}-nacl-default"

  manage_default_security_group  = true
  default_security_group_name    = "${local.name_prefix}-sg-default"
  default_security_group_ingress = []
  default_security_group_egress  = []
}

resource "aws_vpc_endpoint" "s3" {
  vpc_id            = module.vpc.vpc_id
  service_name      = "com.amazonaws.${var.aws_region}.s3"
  vpc_endpoint_type = "Gateway"
  route_table_ids   = concat(module.vpc.private_route_table_ids, module.vpc.intra_route_table_ids)

  # Sin política, el endpoint deja llegar a cualquier bucket, también a los de otras cuentas.
  # El de starport guarda las capas de las imágenes de ECR.
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid       = "ProjectBucket"
        Effect    = "Allow"
        Principal = "*"
        Action    = "s3:*"
        Resource  = [module.storage.bucket_arn, "${module.storage.bucket_arn}/*"]
      },
      {
        Sid       = "EcrLayers"
        Effect    = "Allow"
        Principal = "*"
        Action    = "s3:GetObject"
        Resource  = "arn:aws:s3:::prod-${var.aws_region}-starport-layer-bucket/*"
      },
    ]
  })

  tags = {
    Name = "${local.name_prefix}-vpce-s3"
  }
}

# Sin NAT en las subredes de archive: los hosts de ECS llegan por acá a ECS, ECR, Logs y
# Secrets Manager. Los de app también, por el DNS privado.
resource "aws_vpc_endpoint" "interface" {
  for_each = toset(["ecs", "ecs-agent", "ecs-telemetry", "ecr.api", "ecr.dkr", "logs", "secretsmanager"])

  vpc_id              = module.vpc.vpc_id
  service_name        = "com.amazonaws.${var.aws_region}.${each.key}"
  vpc_endpoint_type   = "Interface"
  subnet_ids          = module.vpc.private_subnets
  security_group_ids  = [module.security_groups.ids.endpoints]
  private_dns_enabled = true

  # Sólo credenciales de esta cuenta: así no sirve para mandar datos a otra.
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Sid       = "OwnAccountPrincipals"
      Effect    = "Allow"
      Principal = "*"
      Action    = "*"
      Resource  = "*"
      Condition = { StringEquals = { "aws:PrincipalAccount" = data.aws_caller_identity.current.account_id } }
    }]
  })

  tags = {
    Name = "${local.name_prefix}-vpce-${replace(each.key, ".", "-")}"
  }
}
