terraform {
  backend "s3" {
    key          = "tp3/terraform.tfstate"
    region       = "us-east-1"
    encrypt      = true
    use_lockfile = true
  }
}
