# database

RDS PostgreSQL Multi-AZ en las subredes de datos, que no tienen ruta por defecto: la base
es inalcanzable desde Internet por ruteo, no sólo por security group.

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
| [aws_db_instance.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/db_instance) | resource |

## Inputs

| Name | Description | Type | Default | Required |
|------|-------------|------|---------|:--------:|
| <a name="input_db_subnet_group_name"></a> [db\_subnet\_group\_name](#input\_db\_subnet\_group\_name) | Subnet group de la capa de datos (lo crea el módulo de VPC). | `string` | n/a | yes |
| <a name="input_engine_version"></a> [engine\_version](#input\_engine\_version) | Versión de PostgreSQL. Con sólo el major, RDS elige el último minor. | `string` | n/a | yes |
| <a name="input_instance_class"></a> [instance\_class](#input\_instance\_class) | Clase de la instancia RDS. | `string` | n/a | yes |
| <a name="input_kms_key_arn"></a> [kms\_key\_arn](#input\_kms\_key\_arn) | Clave KMS del almacenamiento, los backups y los snapshots. Cambiarla recrea la instancia. | `string` | n/a | yes |
| <a name="input_multi_az"></a> [multi\_az](#input\_multi\_az) | Standby sincrónica en la segunda AZ. | `bool` | n/a | yes |
| <a name="input_name_prefix"></a> [name\_prefix](#input\_name\_prefix) | Prefijo del identificador de la instancia RDS, del nombre de la base y del usuario. | `string` | n/a | yes |
| <a name="input_password"></a> [password](#input\_password) | Password del usuario maestro. Sin caracteres especiales: va dentro de la URL de conexión. | `string` | n/a | yes |
| <a name="input_port"></a> [port](#input\_port) | Puerto de PostgreSQL. | `number` | n/a | yes |
| <a name="input_security_group_id"></a> [security\_group\_id](#input\_security\_group\_id) | SG de la capa de datos: el único que lleva la instancia. | `string` | n/a | yes |

## Outputs

| Name | Description |
|------|-------------|
| <a name="output_address"></a> [address](#output\_address) | Hostname de la base. En un failover Multi-AZ apunta a la nueva primaria sin cambiar. |
| <a name="output_url"></a> [url](#output\_url) | DB\_URL de los binarios, con TLS obligatorio. Lleva la password: es sensible. |
<!-- END_TF_DOCS -->
