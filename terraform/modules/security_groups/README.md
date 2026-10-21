# security_groups

Un security group por capa (`nlb`, `app`, `archive`, `data`), más el de los hosts de ECS
(`host`) y el de los interface endpoints (`endpoints`). Se encadenan por referencia entre
grupos: `nlb ← internet`, `app ← nlb`, `data ← app, archive` y `endpoints ← host`. Las salidas
también están acotadas: a otro grupo o a la prefix list de S3, y sólo `app` sale a Internet
(proveedor y webhook, por el NAT). Las reglas salen de una sola tabla en `locals`. Sin SSH y
con las NACLs por defecto.

<!-- BEGIN_TF_DOCS -->
## Requirements

| Name | Version |
|------|---------|
| <a name="requirement_terraform"></a> [terraform](#requirement\_terraform) | >= 1.9 |
| <a name="requirement_aws"></a> [aws](#requirement\_aws) | >= 6.28 |

## Providers

| Name | Version |
|------|---------|
| <a name="provider_aws"></a> [aws](#provider\_aws) | >= 6.28 |

## Modules

No modules.

## Resources

| Name | Type |
|------|------|
| [aws_security_group.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/security_group) | resource |
| [aws_vpc_security_group_egress_rule.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_security_group_egress_rule) | resource |
| [aws_vpc_security_group_ingress_rule.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_security_group_ingress_rule) | resource |

## Inputs

| Name | Description | Type | Default | Required |
|------|-------------|------|---------|:--------:|
| <a name="input_app_port"></a> [app\_port](#input\_app\_port) | Puerto donde escucha la API: destino del NLB y de su health check. | `number` | n/a | yes |
| <a name="input_db_port"></a> [db\_port](#input\_db\_port) | Puerto de PostgreSQL. | `number` | n/a | yes |
| <a name="input_name_prefix"></a> [name\_prefix](#input\_name\_prefix) | Prefijo de los nombres de los security groups. | `string` | n/a | yes |
| <a name="input_s3_prefix_list_id"></a> [s3\_prefix\_list\_id](#input\_s3\_prefix\_list\_id) | Prefix list del gateway endpoint de S3: destino de la salida a S3 de API y archivador. | `string` | n/a | yes |
| <a name="input_vpc_id"></a> [vpc\_id](#input\_vpc\_id) | VPC donde se crean los grupos. | `string` | n/a | yes |

## Outputs

| Name | Description |
|------|-------------|
| <a name="output_ids"></a> [ids](#output\_ids) | ID de cada security group por capa: nlb, app, archive, data, host, endpoints. |
<!-- END_TF_DOCS -->
