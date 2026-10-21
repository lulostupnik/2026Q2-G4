# service

Un servicio de ECS: task definition con red `awsvpc` (cada tarea tiene su ENI y su security
group), el servicio con su capacity provider y, si recibe tráfico, su target group. Escala
las tareas por CPU, por horario o de las dos formas; las instancias las pone el módulo
`capacity`. Los logs del contenedor van a CloudWatch Logs.

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
| [aws_appautoscaling_policy.cpu](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/appautoscaling_policy) | resource |
| [aws_appautoscaling_scheduled_action.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/appautoscaling_scheduled_action) | resource |
| [aws_appautoscaling_target.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/appautoscaling_target) | resource |
| [aws_cloudwatch_log_group.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_log_group) | resource |
| [aws_ecs_service.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ecs_service) | resource |
| [aws_ecs_task_definition.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ecs_task_definition) | resource |
| [aws_region.current](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/region) | data source |

## Inputs

| Name | Description | Type | Default | Required |
|------|-------------|------|---------|:--------:|
| <a name="input_capacity_provider_name"></a> [capacity\_provider\_name](#input\_capacity\_provider\_name) | Capacity provider donde se ubican las tareas. | `string` | n/a | yes |
| <a name="input_cluster_name"></a> [cluster\_name](#input\_cluster\_name) | Cluster de ECS, con el capacity provider del servicio ya asociado. | `string` | n/a | yes |
| <a name="input_command"></a> [command](#input\_command) | Binario que corre el contenedor dentro de la imagen. | `list(string)` | n/a | yes |
| <a name="input_cpu"></a> [cpu](#input\_cpu) | CPU reservada por tarea, en unidades de ECS (1024 = 1 vCPU). Es la base de la métrica de CPU del autoscaling. | `number` | n/a | yes |
| <a name="input_cpu_target"></a> [cpu\_target](#input\_cpu\_target) | CPU promedio, en %, que mantiene el autoscaling. null para no escalar por CPU. | `number` | `null` | no |
| <a name="input_environment"></a> [environment](#input\_environment) | Variables de entorno del contenedor. Sin secretos: quedan a la vista en la task definition. | `map(string)` | `{}` | no |
| <a name="input_image"></a> [image](#input\_image) | Imagen del contenedor, con su tag. | `string` | n/a | yes |
| <a name="input_log_kms_key_arn"></a> [log\_kms\_key\_arn](#input\_log\_kms\_key\_arn) | Clave KMS con la que se cifran los logs. null: la de CloudWatch. | `string` | `null` | no |
| <a name="input_log_retention_days"></a> [log\_retention\_days](#input\_log\_retention\_days) | Días que se conservan los logs del servicio. | `number` | `14` | no |
| <a name="input_max_count"></a> [max\_count](#input\_max\_count) | Máximo de tareas. | `number` | n/a | yes |
| <a name="input_memory"></a> [memory](#input\_memory) | Memoria máxima por tarea, en MiB. | `number` | n/a | yes |
| <a name="input_min_count"></a> [min\_count](#input\_min\_count) | Mínimo de tareas. También es la cantidad con la que arranca el servicio. | `number` | n/a | yes |
| <a name="input_name"></a> [name](#input\_name) | Nombre del servicio, de su task definition y de su log group. | `string` | n/a | yes |
| <a name="input_port"></a> [port](#input\_port) | Puerto del contenedor. null si el servicio no recibe tráfico. | `number` | `null` | no |
| <a name="input_role_arn"></a> [role\_arn](#input\_role\_arn) | Rol de ejecución y de la tarea. En AWS Academy no se pueden crear roles: es el del laboratorio. | `string` | n/a | yes |
| <a name="input_schedule"></a> [schedule](#input\_schedule) | Acciones programadas: fijan la cantidad de tareas a una hora. Cron de Application Auto Scaling. | <pre>map(object({<br/>    schedule = string<br/>    tasks    = number<br/>  }))</pre> | `{}` | no |
| <a name="input_schedule_time_zone"></a> [schedule\_time\_zone](#input\_schedule\_time\_zone) | Zona horaria de las acciones programadas. | `string` | `"America/Argentina/Buenos_Aires"` | no |
| <a name="input_secrets"></a> [secrets](#input\_secrets) | Variables secretas: nombre → ARN de Secrets Manager (arn:clave-json::version-id). ECS lee el valor al arrancar la tarea, con el rol de ejecución. | `map(string)` | `{}` | no |
| <a name="input_security_group_id"></a> [security\_group\_id](#input\_security\_group\_id) | SG de la ENI de cada tarea. | `string` | n/a | yes |
| <a name="input_subnet_ids"></a> [subnet\_ids](#input\_subnet\_ids) | Subredes de las tareas (red awsvpc: cada tarea tiene su ENI). Las mismas que las de sus instancias. | `list(string)` | n/a | yes |
| <a name="input_target_group_arn"></a> [target\_group\_arn](#input\_target\_group\_arn) | Target group donde ECS registra las tareas. null si el servicio no va detrás del balanceador. | `string` | `null` | no |

## Outputs

| Name | Description |
|------|-------------|
| <a name="output_log_group_name"></a> [log\_group\_name](#output\_log\_group\_name) | Log group del servicio: ahí escribe el contenedor. |
| <a name="output_name"></a> [name](#output\_name) | Nombre del servicio de ECS. |
<!-- END_TF_DOCS -->
