# capacity

Instancias EC2 donde corren las tareas de un servicio de ECS: launch template con la AMI
optimizada para ECS, Auto Scaling group y capacity provider con *managed scaling*. El
capacity provider agrega instancias cuando hay tareas sin lugar y apaga las que quedan
vacías, así que el ASG sigue a las tareas y no tiene política de escalado propia.

La AMI sale del parámetro público de SSM según la arquitectura del tipo de instancia (x86 o
arm64, para Graviton). Con `target_capacity` menor a 100 el capacity provider deja instancias
con lugar libre: una tarea nueva, por scale-out o por deploy, arranca sin esperar a que se
lance una instancia. El disco raíz va cifrado y el launch template exige IMDSv2.

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
| [aws_autoscaling_group.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/autoscaling_group) | resource |
| [aws_ecs_capacity_provider.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ecs_capacity_provider) | resource |
| [aws_launch_template.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/launch_template) | resource |
| [aws_ec2_instance_type.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/ec2_instance_type) | data source |
| [aws_ssm_parameter.ami](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/ssm_parameter) | data source |

## Inputs

| Name | Description | Type | Default | Required |
|------|-------------|------|---------|:--------:|
| <a name="input_cluster_name"></a> [cluster\_name](#input\_cluster\_name) | Cluster de ECS al que se unen las instancias. | `string` | n/a | yes |
| <a name="input_instance_profile_arn"></a> [instance\_profile\_arn](#input\_instance\_profile\_arn) | Instance profile de las instancias. Lo necesita el agente para registrarse en el cluster. | `string` | n/a | yes |
| <a name="input_instance_type"></a> [instance\_type](#input\_instance\_type) | Tipo de instancia. La AMI de ECS se elige según su arquitectura (x86 o arm64). | `string` | n/a | yes |
| <a name="input_max_size"></a> [max\_size](#input\_max\_size) | Máximo de instancias del ASG. | `number` | n/a | yes |
| <a name="input_name"></a> [name](#input\_name) | Nombre del launch template, el ASG y el capacity provider. | `string` | n/a | yes |
| <a name="input_security_group_id"></a> [security\_group\_id](#input\_security\_group\_id) | SG de las instancias. Las tareas usan el suyo propio. | `string` | n/a | yes |
| <a name="input_subnet_ids"></a> [subnet\_ids](#input\_subnet\_ids) | Subredes de las instancias. El agente llega a ECS, ECR y CloudWatch Logs por los interface endpoints. | `list(string)` | n/a | yes |
| <a name="input_target_capacity"></a> [target\_capacity](#input\_target\_capacity) | Porcentaje de instancias que el capacity provider mantiene ocupadas. Menos de 100 deja lugar libre. | `number` | `100` | no |

## Outputs

| Name | Description |
|------|-------------|
| <a name="output_autoscaling_group_name"></a> [autoscaling\_group\_name](#output\_autoscaling\_group\_name) | ASG de las instancias. Lo escala el capacity provider según las tareas. |
| <a name="output_capacity_provider_name"></a> [capacity\_provider\_name](#output\_capacity\_provider\_name) | Capacity provider que el servicio usa para ubicar sus tareas. |
<!-- END_TF_DOCS -->
