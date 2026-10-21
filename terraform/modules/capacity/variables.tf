variable "name" {
  description = "Nombre del launch template, el ASG y el capacity provider."
  type        = string
}

variable "cluster_name" {
  description = "Cluster de ECS al que se unen las instancias."
  type        = string
}

variable "subnet_ids" {
  description = "Subredes de las instancias. El agente llega a ECS, ECR y CloudWatch Logs por los interface endpoints."
  type        = list(string)
}

variable "instance_type" {
  description = "Tipo de instancia. La AMI de ECS se elige según su arquitectura (x86 o arm64)."
  type        = string
}

variable "instance_profile_arn" {
  description = "Instance profile de las instancias. Lo necesita el agente para registrarse en el cluster."
  type        = string
}

variable "security_group_id" {
  description = "SG de las instancias. Las tareas usan el suyo propio."
  type        = string
}

variable "max_size" {
  description = "Máximo de instancias del ASG."
  type        = number
}

variable "target_capacity" {
  description = "Porcentaje de instancias que el capacity provider mantiene ocupadas. Menos de 100 deja lugar libre."
  type        = number
  default     = 100

  validation {
    condition     = var.target_capacity >= 1 && var.target_capacity <= 100
    error_message = "target_capacity va de 1 a 100."
  }
}
