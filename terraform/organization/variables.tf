variable "project_name" {
  description = "Prefijo de todos los recursos (ledger-vpc, ledger-nlb, ...). Desde make se fija con PROJECT, que también usa scripts/buckets.sh para los nombres de los buckets."
  type        = string
  default     = "ledger"

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{2,19}$", var.project_name))
    error_message = "Entre 3 y 20 caracteres: minúsculas, números y guiones, empezando por una letra (va en nombres de buckets, de RDS y de Cognito)."
  }
}

variable "aws_region" {
  description = "Región de despliegue. AWS Academy sólo habilita us-east-1."
  type        = string
  default     = "us-east-1"
}

variable "vpc_cidr" {
  description = "CIDR de la VPC. Las subredes se derivan de él en locals.tf."
  type        = string
  default     = "10.0.0.0/16"
}

variable "app_port" {
  description = "Puerto donde escucha la API. Lo usan el SG, el target group y la task definition."
  type        = number
  default     = 8080
}

variable "db_port" {
  description = "Puerto de PostgreSQL. Lo usan los SG y la instancia RDS."
  type        = number
  default     = 5432
}

variable "instance_profile_name" {
  description = "Instance profile preexistente del laboratorio."
  type        = string
  default     = "LabInstanceProfile"
}

variable "lab_role_name" {
  description = "Rol preexistente del laboratorio. Lo usan la Lambda del callback de Cognito y las tareas de ECS (rol de ejecución y de la tarea)."
  type        = string
  default     = "LabRole"
}

variable "api_instance_type" {
  description = "Tipo de instancia de los hosts de la API."
  type        = string
  default     = "c7g.large"
}

variable "archive_instance_type" {
  description = "Tipo de instancia del host del archivador."
  type        = string
  default     = "t3.micro"
}

variable "db_engine_version" {
  description = "Versión de PostgreSQL. Sólo el major: RDS elige el último minor disponible."
  type        = string
  default     = "18"
}

variable "db_instance_class" {
  description = "Clase de la instancia RDS. La más chica: la demo es funcional, no de carga."
  type        = string
  default     = "db.t3.micro"
}

variable "db_multi_az" {
  description = "Standby en la segunda AZ: la base sigue disponible si se cae una zona. En false el apply tarda la mitad: sólo para probar."
  type        = bool
  default     = true
}

variable "db_max_connections" {
  description = "Conexiones a la base que se reparten entre todas las tareas. Tiene que quedar por debajo del max_connections de db_instance_class (unas 80 en db.t3.micro), con margen para conectarse a mano."
  type        = number
  default     = 60
}

variable "image_tag" {
  description = "Tag de la imagen del backend en ECR: el hash del árbol de apps/ledger, así que sólo cambia si cambia el backend. make imagen la sube con ese tag y make plan lo pasa solo."
  type        = string
  default     = "sin-imagen"
}

variable "mock_api_key" {
  description = "Clave del proveedor simulado (/servicio-mock). Vacía: se genera una. Se comparte con el equipo que use este despliegue como proveedor."
  type        = string
  default     = ""
  sensitive   = true
}

variable "proveedor_url" {
  description = "URL del proveedor externo. Vacía: la API usa su propio /servicio-mock detrás del NLB."
  type        = string
  default     = ""
}

variable "proveedor_api_key" {
  description = "Clave del proveedor externo. Vacía: la del mock propio."
  type        = string
  default     = ""
  sensitive   = true
}

variable "webhook_url" {
  description = "Adonde la API avisa las órdenes terminadas. Vacía: no avisa."
  type        = string
  default     = ""
}

variable "webhook_secret" {
  description = "Secreto con el que se firman los avisos. Vacío: se genera uno. Tiene que coincidir con el del que recibe."
  type        = string
  default     = ""
  sensitive   = true
}

variable "archive_schedule" {
  description = "Ventana del archivador: una acción abre (1 tarea) y otra cierra (0). Cron de Application Auto Scaling, hora de Buenos Aires."
  type = map(object({
    schedule = string
    tasks    = number
  }))
  default = {
    abrir  = { schedule = "cron(25 19 * * ? *)", tasks = 1 }
    cerrar = { schedule = "cron(30 21 * * ? *)", tasks = 0 }
  }

  validation {
    condition     = alltrue([for a in values(var.archive_schedule) : contains([0, 1], a.tasks)])
    error_message = "El archivador va de 0 a 1 tarea: tasks tiene que ser 0 o 1."
  }
}
