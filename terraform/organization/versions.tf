terraform {
  required_version = ">= 1.10, < 2.0" # 1.10: use_lockfile del backend S3

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.37" # la versión exacta la fija .terraform.lock.hcl
    }
    archive = {
      source  = "hashicorp/archive"
      version = "~> 2.7"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
  }
}
