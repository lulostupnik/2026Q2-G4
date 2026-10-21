# load_balancer

Network Load Balancer en las subredes públicas, listener TCP:80 y target group con
health check de aplicación sobre `/health`. Los targets son las IP de las tareas de
ECS (red `awsvpc`), no las instancias.

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
| [aws_lb.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lb) | resource |
| [aws_lb_listener.http](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lb_listener) | resource |
| [aws_lb_target_group.api](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lb_target_group) | resource |

## Inputs

| Name | Description | Type | Default | Required |
|------|-------------|------|---------|:--------:|
| <a name="input_app_port"></a> [app\_port](#input\_app\_port) | Puerto de la API en las tareas: destino del tráfico y del health check. | `number` | n/a | yes |
| <a name="input_health_check_path"></a> [health\_check\_path](#input\_health\_check\_path) | Path HTTP que el NLB consulta en cada tarea. | `string` | n/a | yes |
| <a name="input_name_prefix"></a> [name\_prefix](#input\_name\_prefix) | Prefijo de los nombres del NLB y su target group. | `string` | n/a | yes |
| <a name="input_public_subnet_ids"></a> [public\_subnet\_ids](#input\_public\_subnet\_ids) | Subredes públicas donde el NLB tiene un nodo por AZ. | `list(string)` | n/a | yes |
| <a name="input_security_group_id"></a> [security\_group\_id](#input\_security\_group\_id) | SG del NLB. La API sólo acepta tráfico que venga de este grupo. | `string` | n/a | yes |
| <a name="input_vpc_id"></a> [vpc\_id](#input\_vpc\_id) | VPC del target group. | `string` | n/a | yes |

## Outputs

| Name | Description |
|------|-------------|
| <a name="output_dns_name"></a> [dns\_name](#output\_dns\_name) | DNS público del NLB: la URL de la API. |
| <a name="output_target_group_arn"></a> [target\_group\_arn](#output\_target\_group\_arn) | Target group donde ECS registra las tareas de la API, por IP. Sale del listener: así el servicio se crea recién cuando el target group ya está atado al NLB. |
<!-- END_TF_DOCS -->
