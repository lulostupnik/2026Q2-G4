variable "name_prefix" {
  description = "Prefijo de los nombres del NLB y su target group."
  type        = string
}

variable "vpc_id" {
  description = "VPC del target group."
  type        = string
}

variable "public_subnet_ids" {
  description = "Subredes públicas donde el NLB tiene un nodo por AZ."
  type        = list(string)
}

variable "security_group_id" {
  description = "SG del NLB. La API sólo acepta tráfico que venga de este grupo."
  type        = string
}

variable "app_port" {
  description = "Puerto de la API en las tareas: destino del tráfico y del health check."
  type        = number
}

variable "health_check_path" {
  description = "Path HTTP que el NLB consulta en cada tarea."
  type        = string
}
