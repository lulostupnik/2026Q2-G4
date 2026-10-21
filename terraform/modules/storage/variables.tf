variable "name_prefix" {
  description = "Prefijo del nombre del bucket."
  type        = string
}

variable "kms_key_arn" {
  description = "Clave KMS con la que se cifran los objetos (SSE-KMS). Quien lee o escribe necesita permiso sobre ella."
  type        = string
}

variable "noncurrent_version_days" {
  description = "Dias que se conserva una versión remplazada o borrada antes de eliminarla."
  type        = number
  default     = 30
}

variable "transitions" {
  description = "Transiciones de clases de almacenamiento."
  type = map(object({
    prefix = string
    transitions = list(object({
      days          = number
      storage_class = string
    }))
  }))
  default = {
    "transaction-history" = {
      prefix = "transaction-history/"
      transitions = [
        { days = 30, storage_class = "STANDARD_IA" },
        { days = 365, storage_class = "GLACIER" },
      ]
    }
  }
}

