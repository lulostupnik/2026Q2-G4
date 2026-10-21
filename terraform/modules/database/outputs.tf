output "address" {
  description = "Hostname de la base. En un failover Multi-AZ apunta a la nueva primaria sin cambiar."
  value       = aws_db_instance.this.address
}

output "url" {
  description = "DB_URL de los binarios, con TLS obligatorio. Lleva la password: es sensible."
  value = format(
    "postgres://%s:%s@%s:%d/%s?sslmode=require",
    aws_db_instance.this.username,
    var.password,
    aws_db_instance.this.address,
    aws_db_instance.this.port,
    aws_db_instance.this.db_name,
  )
  sensitive = true
}

