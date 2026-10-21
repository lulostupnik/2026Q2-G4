variable "name_prefix" {
  description = "Prefijo de los nombres: pool, dominio de la Hosted UI, Lambda y API."
  type        = string
}

variable "frontend_url" {
  description = "URL HTTP del frontend: adonde la Lambda redirige con los tokens en el fragment."
  type        = string
}

variable "lambda_role_arn" {
  description = "Rol de ejecución de la Lambda. En AWS Academy, el LabRole preexistente."
  type        = string
}

variable "callback_source_dir" {
  description = "Carpeta con el handler.mjs de la Lambda del callback."
  type        = string
}

variable "lambda_runtime" {
  description = "Runtime de la Lambda del callback. Node: el handler no tiene dependencias ni paso de build."
  type        = string
  default     = "nodejs22.x"
}

variable "self_sign_up" {
  description = "Si la Hosted UI deja registrarse. En false, los usuarios los crea un admin."
  type        = bool
  default     = true
}

variable "token_validity_hours" {
  description = "Vida del access token y del ID token, en horas (1 a 24). No se entrega refresh token al frontend."
  type        = number
  default     = 8

  validation {
    condition     = var.token_validity_hours >= 1 && var.token_validity_hours <= 24
    error_message = "Cognito acepta entre 1 y 24 horas."
  }
}

variable "kms_key_arn" {
  description = "Clave KMS de los logs (Lambda y access logs de API Gateway) y del secreto con el client secret. null: las que administra AWS."
  type        = string
  default     = null
}

variable "log_retention_days" {
  description = "Días que se conservan los logs de la Lambda y los access logs de API Gateway."
  type        = number
  default     = 7

  validation {
    condition     = contains([1, 3, 5, 7, 14, 30, 60, 90, 120, 150, 180, 365, 400, 545, 731, 1096, 1827, 2192, 2557, 2922, 3288, 3653], var.log_retention_days)
    error_message = "CloudWatch Logs sólo acepta 1, 3, 5, 7, 14, 30, 60, 90, 120, 150, 180, 365, 400, 545, 731, 1096, 1827, 2192, 2557, 2922, 3288 o 3653 días."
  }
}
