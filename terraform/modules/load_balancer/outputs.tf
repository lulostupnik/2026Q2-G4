output "dns_name" {
  description = "DNS público del NLB: la URL de la API."
  value       = aws_lb.this.dns_name
}

output "target_group_arn" {
  description = "Target group donde ECS registra las tareas de la API, por IP. Sale del listener: así el servicio se crea recién cuando el target group ya está atado al NLB."
  value       = aws_lb_listener.http.default_action[0].target_group_arn
}
