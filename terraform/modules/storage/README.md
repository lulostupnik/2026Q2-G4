# storage

Bucket S3 del proyecto: guarda los meses archivados del ledger (`transaction-history/`) y las
exportaciones (`exports/`). Se accede por el gateway endpoint, sin pasar por el NAT. Se cifra con la clave KMS del proyecto (SSE-KMS) y su
política rechaza los pedidos que no usan HTTPS.

El bucket no lo crea el módulo sino [`scripts/buckets.sh`](../../../scripts/buckets.sh), antes del
apply: el SCP de AWS Academy niega `s3:GetBucketObjectLockConfiguration`, que `aws_s3_bucket`
lee cada vez que refresca el bucket. El módulo arma el mismo nombre y lo configura.

<!-- BEGIN_TF_DOCS -->
## Requirements

| Name | Version |
|------|---------|
| <a name="requirement_terraform"></a> [terraform](#requirement\_terraform) | >= 1.9 |
| <a name="requirement_aws"></a> [aws](#requirement\_aws) | >= 6.37 |

## Providers

| Name | Version |
|------|---------|
| <a name="provider_aws"></a> [aws](#provider\_aws) | >= 6.37 |

## Modules

No modules.

## Resources

| Name | Type |
|------|------|
| [aws_s3_bucket_lifecycle_configuration.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket_lifecycle_configuration) | resource |
| [aws_s3_bucket_policy.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket_policy) | resource |
| [aws_s3_bucket_public_access_block.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket_public_access_block) | resource |
| [aws_s3_bucket_server_side_encryption_configuration.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket_server_side_encryption_configuration) | resource |
| [aws_s3_bucket_versioning.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket_versioning) | resource |
| [aws_caller_identity.current](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/caller_identity) | data source |
| [aws_region.current](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/region) | data source |

## Inputs

| Name | Description | Type | Default | Required |
|------|-------------|------|---------|:--------:|
| <a name="input_kms_key_arn"></a> [kms\_key\_arn](#input\_kms\_key\_arn) | Clave KMS con la que se cifran los objetos (SSE-KMS). Quien lee o escribe necesita permiso sobre ella. | `string` | n/a | yes |
| <a name="input_name_prefix"></a> [name\_prefix](#input\_name\_prefix) | Prefijo del nombre del bucket. | `string` | n/a | yes |
| <a name="input_noncurrent_version_days"></a> [noncurrent\_version\_days](#input\_noncurrent\_version\_days) | Dias que se conserva una versión remplazada o borrada antes de eliminarla. | `number` | `30` | no |
| <a name="input_transitions"></a> [transitions](#input\_transitions) | Transiciones de clases de almacenamiento. | <pre>map(object({<br/>    prefix = string<br/>    transitions = list(object({<br/>      days          = number<br/>      storage_class = string<br/>    }))<br/>  }))</pre> | <pre>{<br/>  "transaction-history": {<br/>    "prefix": "transaction-history/",<br/>    "transitions": [<br/>      {<br/>        "days": 30,<br/>        "storage_class": "STANDARD_IA"<br/>      },<br/>      {<br/>        "days": 365,<br/>        "storage_class": "GLACIER"<br/>      }<br/>    ]<br/>  }<br/>}</pre> | no |

## Outputs

| Name | Description |
|------|-------------|
| <a name="output_bucket_arn"></a> [bucket\_arn](#output\_bucket\_arn) | ARN del bucket |
| <a name="output_bucket_name"></a> [bucket\_name](#output\_bucket\_name) | Nombre del bucket |
<!-- END_TF_DOCS -->
