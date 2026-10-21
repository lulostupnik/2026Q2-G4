output "name" {
  description = "Nombre del servicio de ECS."
  value       = aws_ecs_service.this.name
}

output "log_group_name" {
  description = "Log group del servicio: ahí escribe el contenedor."
  value       = aws_cloudwatch_log_group.this.name
}
