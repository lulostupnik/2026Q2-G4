variable "name_prefix" {
  description = "Prefijo del identificador de la instancia RDS, del nombre de la base y del usuario."
  type        = string
}

variable "db_subnet_group_name" {
  description = "Subnet group de la capa de datos (lo crea el módulo de VPC)."
  type        = string
}

variable "security_group_id" {
  description = "SG de la capa de datos: el único que lleva la instancia."
  type        = string
}

variable "password" {
  description = "Password del usuario maestro. Sin caracteres especiales: va dentro de la URL de conexión."
  type        = string
  sensitive   = true
}

variable "kms_key_arn" {
  description = "Clave KMS del almacenamiento, los backups y los snapshots. Cambiarla recrea la instancia."
  type        = string
}

variable "port" {
  description = "Puerto de PostgreSQL."
  type        = number
}

variable "engine_version" {
  description = "Versión de PostgreSQL. Con sólo el major, RDS elige el último minor."
  type        = string
}

variable "instance_class" {
  description = "Clase de la instancia RDS."
  type        = string
}

variable "multi_az" {
  description = "Standby sincrónica en la segunda AZ."
  type        = bool
}
