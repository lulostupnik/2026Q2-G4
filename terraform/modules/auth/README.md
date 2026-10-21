# auth

Login con Cognito. El frontend se sirve por HTTP desde S3, y Cognito sólo acepta callbacks
HTTPS. Por eso el callback es una ruta de API Gateway que
invoca una Lambda: la Lambda canjea el `code` por tokens y redirige al frontend con los
tokens en el fragment (`#access_token=…`).

```
frontend (HTTP) → login_url (Hosted UI) → GET <api>/callback?code&state
                → Lambda: POST /oauth2/token → 302 <frontend>/#access_token=…&state=…
frontend → logout_url (Hosted UI) → GET <api>/logout → 302 <frontend>/
```

- El app client es **confidencial**: el secret está en Secrets Manager (`<prefijo>/callback`) y
  la Lambda sólo tiene su ARN. Lo lee una vez por instancia, en el primer login.
- No se le entrega refresh token al frontend. Cuando el access token vence, el frontend vuelve
  a la pantalla de ingreso; al tocar **Ingresar** va a `login_url`, y si la sesión de la Hosted
  UI sigue viva, el usuario entra sin tipear nada.
- `/logout` existe porque Cognito también exige HTTPS en las logout URLs. Sin ese paso, la
  cookie de la Hosted UI sigue viva y no se puede cambiar de usuario.

<!-- BEGIN_TF_DOCS -->
## Requirements

| Name | Version |
|------|---------|
| <a name="requirement_terraform"></a> [terraform](#requirement\_terraform) | >= 1.9 |
| <a name="requirement_archive"></a> [archive](#requirement\_archive) | >= 2.4 |
| <a name="requirement_aws"></a> [aws](#requirement\_aws) | >= 6.28 |

## Providers

| Name | Version |
|------|---------|
| <a name="provider_archive"></a> [archive](#provider\_archive) | >= 2.4 |
| <a name="provider_aws"></a> [aws](#provider\_aws) | >= 6.28 |

## Modules

No modules.

## Resources

| Name | Type |
|------|------|
| [aws_apigatewayv2_api.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/apigatewayv2_api) | resource |
| [aws_apigatewayv2_integration.callback](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/apigatewayv2_integration) | resource |
| [aws_apigatewayv2_route.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/apigatewayv2_route) | resource |
| [aws_apigatewayv2_stage.default](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/apigatewayv2_stage) | resource |
| [aws_cloudwatch_log_group.api](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_log_group) | resource |
| [aws_cloudwatch_log_group.callback](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_log_group) | resource |
| [aws_cognito_user_pool.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cognito_user_pool) | resource |
| [aws_cognito_user_pool_client.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cognito_user_pool_client) | resource |
| [aws_cognito_user_pool_domain.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cognito_user_pool_domain) | resource |
| [aws_lambda_function.callback](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_function) | resource |
| [aws_lambda_permission.api_gateway](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_permission) | resource |
| [aws_secretsmanager_secret.client](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/secretsmanager_secret) | resource |
| [aws_secretsmanager_secret_version.client](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/secretsmanager_secret_version) | resource |
| [archive_file.callback](https://registry.terraform.io/providers/hashicorp/archive/latest/docs/data-sources/file) | data source |
| [aws_caller_identity.current](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/caller_identity) | data source |
| [aws_region.current](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/region) | data source |

## Inputs

| Name | Description | Type | Default | Required |
|------|-------------|------|---------|:--------:|
| <a name="input_callback_source_dir"></a> [callback\_source\_dir](#input\_callback\_source\_dir) | Carpeta con el handler.mjs de la Lambda del callback. | `string` | n/a | yes |
| <a name="input_frontend_url"></a> [frontend\_url](#input\_frontend\_url) | URL HTTP del frontend: adonde la Lambda redirige con los tokens en el fragment. | `string` | n/a | yes |
| <a name="input_kms_key_arn"></a> [kms\_key\_arn](#input\_kms\_key\_arn) | Clave KMS de los logs (Lambda y access logs de API Gateway) y del secreto con el client secret. null: las que administra AWS. | `string` | `null` | no |
| <a name="input_lambda_role_arn"></a> [lambda\_role\_arn](#input\_lambda\_role\_arn) | Rol de ejecución de la Lambda. En AWS Academy, el LabRole preexistente. | `string` | n/a | yes |
| <a name="input_lambda_runtime"></a> [lambda\_runtime](#input\_lambda\_runtime) | Runtime de la Lambda del callback. Node: el handler no tiene dependencias ni paso de build. | `string` | `"nodejs22.x"` | no |
| <a name="input_log_retention_days"></a> [log\_retention\_days](#input\_log\_retention\_days) | Días que se conservan los logs de la Lambda y los access logs de API Gateway. | `number` | `7` | no |
| <a name="input_name_prefix"></a> [name\_prefix](#input\_name\_prefix) | Prefijo de los nombres: pool, dominio de la Hosted UI, Lambda y API. | `string` | n/a | yes |
| <a name="input_self_sign_up"></a> [self\_sign\_up](#input\_self\_sign\_up) | Si la Hosted UI deja registrarse. En false, los usuarios los crea un admin. | `bool` | `true` | no |
| <a name="input_token_validity_hours"></a> [token\_validity\_hours](#input\_token\_validity\_hours) | Vida del access token y del ID token, en horas (1 a 24). No se entrega refresh token al frontend. | `number` | `8` | no |

## Outputs

| Name | Description |
|------|-------------|
| <a name="output_callback_url"></a> [callback\_url](#output\_callback\_url) | Callback HTTPS registrado en Cognito (API Gateway → Lambda). |
| <a name="output_client_id"></a> [client\_id](#output\_client\_id) | App client. La API lo compara con el claim client\_id del access token. |
| <a name="output_issuer"></a> [issuer](#output\_issuer) | Issuer de los tokens. La API valida contra <issuer>/.well-known/jwks.json. |
| <a name="output_login_url"></a> [login\_url](#output\_login\_url) | Hosted UI lista para usar. El frontend le agrega &state=<random> antes de redirigir. |
| <a name="output_logout_url"></a> [logout\_url](#output\_logout\_url) | Cierra la sesión de la Hosted UI y vuelve al frontend pasando por /logout. |
| <a name="output_user_pool_id"></a> [user\_pool\_id](#output\_user\_pool\_id) | ID del user pool. Para crear usuarios por CLI (admin-create-user). |
<!-- END_TF_DOCS -->
