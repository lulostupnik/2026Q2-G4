variable "name_prefix" {
  description = "Prefijo del nombre del bucket."
  type        = string
}

variable "site_dir" {
  description = "Carpeta local con los archivos del frontend. Si no existe, el bucket queda vacío."
  type        = string
}

variable "runtime_config" {
  description = "Claves que se publican en config.json para el frontend. Vacío: no se publica."
  type        = map(string)
  default     = {}
}

variable "index_document" {
  description = "Documento que sirve el endpoint de website al pedir una carpeta."
  type        = string
  default     = "index.html"
}

variable "content_types" {
  description = "MIME por extensión. Sólo se publican los archivos cuya extensión está acá."
  type        = map(string)
  default = {
    html  = "text/html; charset=utf-8"
    css   = "text/css; charset=utf-8"
    js    = "text/javascript; charset=utf-8" # los módulos ES se rechazan con otro MIME
    json  = "application/json"
    svg   = "image/svg+xml"
    png   = "image/png"
    jpg   = "image/jpeg"
    jpeg  = "image/jpeg"
    ico   = "image/x-icon"
    woff2 = "font/woff2"
  }
}
