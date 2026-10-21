output "ids" {
  description = "ID de cada security group por capa: nlb, app, archive, data, host, endpoints."
  value       = { for k, sg in aws_security_group.this : k => sg.id }
}
