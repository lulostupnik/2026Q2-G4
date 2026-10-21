locals {
  groups = {
    nlb       = "NLB: HTTP desde Internet, sale solo hacia la API"
    app       = "API: entra desde el NLB, sale a la base, a S3 y al proveedor"
    archive   = "Archivador: sin entrada, sale solo a la base y a S3"
    data      = "RDS: entra solo desde la API y el archivador"
    host      = "Hosts de ECS: sin entrada, salen solo a los endpoints de ECS, ECR, Logs y Secrets Manager, y a S3"
    endpoints = "Interface endpoints de ECS, ECR, Logs y Secrets Manager: entran solo desde los hosts de ECS"
  }

  rules = {
    nlb_in_internet = { sg = "nlb", type = "ingress", port = 80, peer = "internet", description = "HTTP desde Internet" }
    nlb_out_app     = { sg = "nlb", type = "egress", port = var.app_port, peer = "app", description = "Hacia la API: trafico y health check" }

    app_in_nlb    = { sg = "app", type = "ingress", port = var.app_port, peer = "nlb", description = "Solo desde el NLB" }
    app_out_data  = { sg = "app", type = "egress", port = var.db_port, peer = "data", description = "Hacia la base" }
    app_out_s3    = { sg = "app", type = "egress", port = 443, peer = "s3", description = "S3 por el gateway endpoint" }
    app_out_https = { sg = "app", type = "egress", port = 443, peer = "internet", description = "Proveedor y webhook por HTTPS, via NAT" }
    app_out_http  = { sg = "app", type = "egress", port = 80, peer = "internet", description = "Proveedor y webhook de otro equipo: su balanceador es HTTP, via NAT" }

    archive_out_data = { sg = "archive", type = "egress", port = var.db_port, peer = "data", description = "Hacia la base" }
    archive_out_s3   = { sg = "archive", type = "egress", port = 443, peer = "s3", description = "S3 por el gateway endpoint" }

    data_in_app     = { sg = "data", type = "ingress", port = var.db_port, peer = "app", description = "Desde la API" }
    data_in_archive = { sg = "data", type = "ingress", port = var.db_port, peer = "archive", description = "Desde el archivador" }

    host_out_endpoints = { sg = "host", type = "egress", port = 443, peer = "endpoints", description = "Agente de ECS, ECR, CloudWatch Logs y Secrets Manager por los interface endpoints" }
    host_out_s3        = { sg = "host", type = "egress", port = 443, peer = "s3", description = "Capas de la imagen: ECR las sirve desde S3" }

    endpoints_in_host = { sg = "endpoints", type = "ingress", port = 443, peer = "host", description = "Desde los hosts de ECS" }
  }
}

resource "aws_security_group" "this" {
  for_each = local.groups

  name        = "${var.name_prefix}-sg-${each.key}"
  description = each.value
  vpc_id      = var.vpc_id

  tags = {
    Name = "${var.name_prefix}-sg-${each.key}"
  }
}

resource "aws_vpc_security_group_ingress_rule" "this" {
  for_each = { for k, r in local.rules : k => r if r.type == "ingress" }

  security_group_id = aws_security_group.this[each.value.sg].id
  description       = each.value.description
  ip_protocol       = "tcp"
  from_port         = each.value.port
  to_port           = each.value.port

  cidr_ipv4                    = each.value.peer == "internet" ? "0.0.0.0/0" : null
  referenced_security_group_id = try(aws_security_group.this[each.value.peer].id, null)

  tags = {
    Name = "${var.name_prefix}-${replace(each.key, "_", "-")}"
  }
}

resource "aws_vpc_security_group_egress_rule" "this" {
  for_each = { for k, r in local.rules : k => r if r.type == "egress" }

  security_group_id = aws_security_group.this[each.value.sg].id
  description       = each.value.description
  ip_protocol       = "tcp"
  from_port         = each.value.port
  to_port           = each.value.port

  cidr_ipv4                    = each.value.peer == "internet" ? "0.0.0.0/0" : null
  prefix_list_id               = each.value.peer == "s3" ? var.s3_prefix_list_id : null
  referenced_security_group_id = try(aws_security_group.this[each.value.peer].id, null)

  tags = {
    Name = "${var.name_prefix}-${replace(each.key, "_", "-")}"
  }
}
