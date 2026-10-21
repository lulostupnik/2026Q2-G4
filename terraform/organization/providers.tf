provider "aws" {
  region = var.aws_region

  default_tags {
    tags = local.common_tags
  }
}

data "aws_availability_zones" "available" {
  state = "available"
}

data "aws_caller_identity" "current" {}

# AWS Academy no permite crear IAM: las instancias usan el perfil que trae el laboratorio.
data "aws_iam_instance_profile" "lab" {
  name = var.instance_profile_name
}

# Mismo motivo: la Lambda del callback de Cognito y las tareas de ECS corren con el rol del laboratorio.
data "aws_iam_role" "lab" {
  name = var.lab_role_name
}
