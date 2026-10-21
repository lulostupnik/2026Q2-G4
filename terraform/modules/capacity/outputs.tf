output "capacity_provider_name" {
  description = "Capacity provider que el servicio usa para ubicar sus tareas."
  value       = aws_ecs_capacity_provider.this.name
}

output "autoscaling_group_name" {
  description = "ASG de las instancias. Lo escala el capacity provider según las tareas."
  value       = aws_autoscaling_group.this.name
}
