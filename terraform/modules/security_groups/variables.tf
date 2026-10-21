variable "name_prefix" {
  description = "Prefijo de los nombres de los security groups."
  type        = string
}

variable "vpc_id" {
  description = "VPC donde se crean los grupos."
  type        = string
}

variable "app_port" {
  description = "Puerto donde escucha la API: destino del NLB y de su health check."
  type        = number
}

variable "db_port" {
  description = "Puerto de PostgreSQL."
  type        = number
}

variable "s3_prefix_list_id" {
  description = "Prefix list del gateway endpoint de S3: destino de la salida a S3 de API y archivador."
  type        = string
}
