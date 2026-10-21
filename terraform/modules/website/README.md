# website

Bucket S3 público con website hosting para el frontend estático. Se sirve por HTTP, porque
el website endpoint de S3 no tiene HTTPS; el HTTPS que Cognito exige para el callback lo pone
API Gateway (ver el módulo `auth`).
Sube cada archivo de `site_dir` con su `Content-Type`, y sólo publica las extensiones
declaradas en `content_types`. Además publica `config.json` a partir de `runtime_config`:
la URL de la API y las de login y logout de Cognito, que no existen hasta el apply.

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
| [aws_s3_bucket_policy.public_read](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket_policy) | resource |
| [aws_s3_bucket_public_access_block.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket_public_access_block) | resource |
| [aws_s3_bucket_server_side_encryption_configuration.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket_server_side_encryption_configuration) | resource |
| [aws_s3_bucket_website_configuration.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket_website_configuration) | resource |
| [aws_s3_object.config](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_object) | resource |
| [aws_s3_object.site](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_object) | resource |
| [aws_caller_identity.current](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/caller_identity) | data source |
| [aws_region.current](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/region) | data source |

## Inputs

| Name | Description | Type | Default | Required |
|------|-------------|------|---------|:--------:|
| <a name="input_content_types"></a> [content\_types](#input\_content\_types) | MIME por extensión. Sólo se publican los archivos cuya extensión está acá. | `map(string)` | <pre>{<br/>  "css": "text/css; charset=utf-8",<br/>  "html": "text/html; charset=utf-8",<br/>  "ico": "image/x-icon",<br/>  "jpeg": "image/jpeg",<br/>  "jpg": "image/jpeg",<br/>  "js": "text/javascript; charset=utf-8",<br/>  "json": "application/json",<br/>  "png": "image/png",<br/>  "svg": "image/svg+xml",<br/>  "woff2": "font/woff2"<br/>}</pre> | no |
| <a name="input_index_document"></a> [index\_document](#input\_index\_document) | Documento que sirve el endpoint de website al pedir una carpeta. | `string` | `"index.html"` | no |
| <a name="input_name_prefix"></a> [name\_prefix](#input\_name\_prefix) | Prefijo del nombre del bucket. | `string` | n/a | yes |
| <a name="input_runtime_config"></a> [runtime\_config](#input\_runtime\_config) | Claves que se publican en config.json para el frontend. Vacío: no se publica. | `map(string)` | `{}` | no |
| <a name="input_site_dir"></a> [site\_dir](#input\_site\_dir) | Carpeta local con los archivos del frontend. Si no existe, el bucket queda vacío. | `string` | n/a | yes |

## Outputs

| Name | Description |
|------|-------------|
| <a name="output_bucket_name"></a> [bucket\_name](#output\_bucket\_name) | Nombre del bucket del frontend. |
| <a name="output_website_url"></a> [website\_url](#output\_website\_url) | URL HTTP del frontend (destino del redirect de la Lambda de Cognito). |
<!-- END_TF_DOCS -->
