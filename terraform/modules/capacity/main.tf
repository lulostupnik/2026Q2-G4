data "aws_ec2_instance_type" "this" {
  instance_type = var.instance_type
}

data "aws_ssm_parameter" "ami" {
  name = format(
    "/aws/service/ecs/optimized-ami/amazon-linux-2023/%srecommended/image_id",
    contains(data.aws_ec2_instance_type.this.supported_architectures, "arm64") ? "arm64/" : "",
  )
}

resource "aws_launch_template" "this" {
  name                   = var.name
  image_id               = data.aws_ssm_parameter.ami.insecure_value
  instance_type          = var.instance_type
  vpc_security_group_ids = [var.security_group_id]
  user_data              = base64encode(templatefile("${path.module}/templates/ecs.sh.tftpl", { cluster = var.cluster_name }))

  iam_instance_profile {
    arn = var.instance_profile_arn
  }

  block_device_mappings {
    device_name = "/dev/xvda"

    ebs {
      encrypted             = true
      volume_type           = "gp3"
      delete_on_termination = true
    }
  }

  metadata_options {
    http_endpoint               = "enabled"
    http_tokens                 = "required"
    http_put_response_hop_limit = 2
  }

  tag_specifications {
    resource_type = "instance"
    tags          = { Name = "${var.name}-host" }
  }
}

# Protegidas contra scale-in: el capacity provider sólo apaga instancias sin tareas.
resource "aws_autoscaling_group" "this" {
  name                  = var.name
  min_size              = 0
  max_size              = var.max_size
  vpc_zone_identifier   = var.subnet_ids
  health_check_type     = "EC2"
  protect_from_scale_in = true
  force_delete          = true

  launch_template {
    id      = aws_launch_template.this.id
    version = "$Latest"
  }

  tag {
    key                 = "Name"
    value               = "${var.name}-host"
    propagate_at_launch = true
  }

  # ECS lo agrega al asociar el capacity provider: declarado para que Terraform no lo borre.
  tag {
    key                 = "AmazonECSManaged"
    value               = "true"
    propagate_at_launch = true
  }

  lifecycle {
    ignore_changes = [desired_capacity]
  }
}

resource "aws_ecs_capacity_provider" "this" {
  name = var.name

  auto_scaling_group_provider {
    auto_scaling_group_arn         = aws_autoscaling_group.this.arn
    managed_termination_protection = "ENABLED"

    managed_scaling {
      status                    = "ENABLED"
      target_capacity           = var.target_capacity
      minimum_scaling_step_size = 1
      maximum_scaling_step_size = 2
    }
  }
}
