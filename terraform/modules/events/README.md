# events

Eventos del ledger. La API publica cada orden terminada en el topic `events`, que los reparte:

- **Webhook:** SNS → SQS (+DLQ) → Lambda, que firma el aviso con HMAC y hace el POST. Los
  reintentos son de SQS; lo que los agota queda en la DLQ y dispara una alarma.
- **Alertas:** topic aparte, con suscripciones de email para operación.
- **Titulares:** topic `titulares`, con una suscripción de email por titular. Lo usa la API para
  avisarle a cada uno lo suyo (hoy, que su exportación está lista).

## Contrato del evento

La API publica en `SNS_TOPIC_ARN` un mensaje por orden que llega a un estado terminal.

- **Atributos del mensaje** (`String`): `tipo` y `estado`, para filtrar suscripciones sin leer
  el cuerpo.
- **Cuerpo** (JSON), que la Lambda firma y entrega tal cual:

```json
{
  "evento_id": "42:liquidada",
  "orden_id": 42,
  "tipo": "transferencia",
  "estado": "liquidada",
  "motivo": "",
  "origen_id": 7,
  "destino_id": 9,
  "monto_centavos": 150050,
  "referencia": "alquiler"
}
```

`evento_id` es `<orden_id>:<estado>`: el mismo evento tiene siempre el mismo id, así que el
receptor del webhook puede descartar los duplicados.

## Contrato de los avisos a titulares

La API suscribe el email de cada titular a `TITULARES_TOPIC_ARN` con el *filter policy*
`{"titular": ["<sub de Cognito>"]}`, y publica cada aviso con el atributo `titular` (`String`)
igual a ese `sub`: SNS se lo entrega sólo a esa suscripción. El cuerpo es texto plano, porque
llega como mail. SNS admite 200 *filter policies* por topic.

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
| [aws_cloudwatch_log_group.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_log_group) | resource |
| [aws_cloudwatch_metric_alarm.webhook_dlq](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_metric_alarm) | resource |
| [aws_lambda_event_source_mapping.webhook](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_event_source_mapping) | resource |
| [aws_lambda_function.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_function) | resource |
| [aws_secretsmanager_secret.webhook](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/secretsmanager_secret) | resource |
| [aws_secretsmanager_secret_version.webhook](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/secretsmanager_secret_version) | resource |
| [aws_sns_topic.alerts](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sns_topic) | resource |
| [aws_sns_topic.events](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sns_topic) | resource |
| [aws_sns_topic_subscription.alerts](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sns_topic_subscription) | resource |
| [aws_sns_topic_subscription.webhook](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sns_topic_subscription) | resource |
| [aws_sqs_queue.webhook](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sqs_queue) | resource |
| [aws_sqs_queue.webhook_dlq](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sqs_queue) | resource |
| [aws_sqs_queue_policy.webhook](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sqs_queue_policy) | resource |
| [archive_file.this](https://registry.terraform.io/providers/hashicorp/archive/latest/docs/data-sources/file) | data source |

## Inputs

| Name | Description | Type | Default | Required |
|------|-------------|------|---------|:--------:|
| <a name="input_kms_key_arn"></a> [kms\_key\_arn](#input\_kms\_key\_arn) | Clave KMS de los topics, las colas, los logs y el secreto. Su política tiene que habilitar a sns.amazonaws.com y cloudwatch.amazonaws.com. | `string` | n/a | yes |
| <a name="input_lambda_role_arn"></a> [lambda\_role\_arn](#input\_lambda\_role\_arn) | Rol de ejecución de la Lambda. En AWS Academy, el LabRole preexistente. | `string` | n/a | yes |
| <a name="input_lambda_runtime"></a> [lambda\_runtime](#input\_lambda\_runtime) | Runtime de la Lambda. Node: el SDK de AWS viene en el runtime, sin dependencias ni paso de build. | `string` | `"nodejs22.x"` | no |
| <a name="input_lambda_timeout"></a> [lambda\_timeout](#input\_lambda\_timeout) | Timeout de la Lambda, en segundos. La visibilidad de la cola del webhook es 6 veces este valor. | `number` | `10` | no |
| <a name="input_log_retention_days"></a> [log\_retention\_days](#input\_log\_retention\_days) | Días que se conservan los logs de la Lambda. | `number` | `7` | no |
| <a name="input_name_prefix"></a> [name\_prefix](#input\_name\_prefix) | Prefijo de los nombres: topics, colas, Lambda y secreto. | `string` | n/a | yes |
| <a name="input_notification_emails"></a> [notification\_emails](#input\_notification\_emails) | Emails que se suscriben al topic de alertas de operación. | `list(string)` | `[]` | no |
| <a name="input_webhook_max_attempts"></a> [webhook\_max\_attempts](#input\_webhook\_max\_attempts) | Cuántas veces se intenta entregar un aviso antes de mandarlo a la DLQ. | `number` | `5` | no |
| <a name="input_webhook_secret"></a> [webhook\_secret](#input\_webhook\_secret) | Secreto HMAC de los avisos. Tiene que coincidir con el del que recibe. | `string` | n/a | yes |
| <a name="input_webhook_source_dir"></a> [webhook\_source\_dir](#input\_webhook\_source\_dir) | Carpeta con el handler.mjs de la Lambda del webhook. | `string` | n/a | yes |
| <a name="input_webhook_url"></a> [webhook\_url](#input\_webhook\_url) | Adonde se entregan los avisos. Vacía: la Lambda descarta los mensajes. | `string` | `""` | no |

## Outputs

| Name | Description |
|------|-------------|
| <a name="output_alerts_topic_arn"></a> [alerts\_topic\_arn](#output\_alerts\_topic\_arn) | Topic de alertas de operación: la alarma de la DLQ del webhook. |
| <a name="output_topic_arn"></a> [topic\_arn](#output\_topic\_arn) | Topic donde la API publica cada orden terminada (SNS\_TOPIC\_ARN). |
| <a name="output_webhook_dlq_url"></a> [webhook\_dlq\_url](#output\_webhook\_dlq\_url) | DLQ del webhook: los avisos que agotaron los reintentos. |
| <a name="output_webhook_queue_url"></a> [webhook\_queue\_url](#output\_webhook\_queue\_url) | Cola de entrega del webhook. |
<!-- END_TF_DOCS -->
