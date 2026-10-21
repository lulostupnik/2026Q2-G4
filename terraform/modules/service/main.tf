data "aws_region" "current" {}

resource "aws_cloudwatch_log_group" "this" {
  name              = "/ecs/${var.name}"
  retention_in_days = var.log_retention_days
  kms_key_id        = var.log_kms_key_arn
}

resource "aws_ecs_task_definition" "this" {
  family                   = var.name
  requires_compatibilities = ["EC2"]
  network_mode             = "awsvpc"
  execution_role_arn       = var.role_arn
  task_role_arn            = var.role_arn

  container_definitions = jsonencode([{
    name        = var.name
    image       = var.image
    command     = var.command
    essential   = true
    cpu         = var.cpu
    memory      = var.memory
    stopTimeout = 30
    portMappings = var.port == null ? [] : [{
      containerPort = var.port
      protocol      = "tcp"
    }]
    environment = [for nombre in sort(keys(var.environment)) : { name = nombre, value = var.environment[nombre] }]
    secrets     = [for nombre in sort(keys(var.secrets)) : { name = nombre, valueFrom = var.secrets[nombre] }]
    logConfiguration = {
      logDriver = "awslogs"
      options = {
        awslogs-group         = aws_cloudwatch_log_group.this.name
        awslogs-region        = data.aws_region.current.region
        awslogs-stream-prefix = var.name
      }
    }
  }])
}

resource "aws_ecs_service" "this" {
  name                              = var.name
  cluster                           = var.cluster_name
  task_definition                   = aws_ecs_task_definition.this.arn
  desired_count                     = var.min_count
  health_check_grace_period_seconds = var.target_group_arn == null ? null : 90

  deployment_minimum_healthy_percent = 50
  deployment_maximum_percent         = 200

  deployment_circuit_breaker {
    enable   = true
    rollback = true
  }

  capacity_provider_strategy {
    capacity_provider = var.capacity_provider_name
    weight            = 1
  }

  network_configuration {
    subnets          = var.subnet_ids
    security_groups  = [var.security_group_id]
    assign_public_ip = false
  }

  ordered_placement_strategy {
    type  = "spread"
    field = "attribute:ecs.availability-zone"
  }

  dynamic "load_balancer" {
    for_each = var.target_group_arn == null ? [] : [var.target_group_arn]
    content {
      target_group_arn = load_balancer.value
      container_name   = var.name
      container_port   = var.port
    }
  }

  lifecycle {
    ignore_changes = [desired_count]
  }
}

resource "aws_appautoscaling_target" "this" {
  service_namespace  = "ecs"
  resource_id        = "service/${var.cluster_name}/${aws_ecs_service.this.name}"
  scalable_dimension = "ecs:service:DesiredCount"
  min_capacity       = var.min_count
  max_capacity       = var.max_count
}

resource "aws_appautoscaling_policy" "cpu" {
  count = var.cpu_target == null ? 0 : 1

  name               = "${var.name}-cpu"
  service_namespace  = aws_appautoscaling_target.this.service_namespace
  resource_id        = aws_appautoscaling_target.this.resource_id
  scalable_dimension = aws_appautoscaling_target.this.scalable_dimension
  policy_type        = "TargetTrackingScaling"

  target_tracking_scaling_policy_configuration {
    predefined_metric_specification {
      predefined_metric_type = "ECSServiceAverageCPUUtilization"
    }
    target_value       = var.cpu_target
    scale_in_cooldown  = 300
    scale_out_cooldown = 90
  }
}

resource "aws_appautoscaling_scheduled_action" "this" {
  for_each = var.schedule

  name               = "${var.name}-${each.key}"
  service_namespace  = aws_appautoscaling_target.this.service_namespace
  resource_id        = aws_appautoscaling_target.this.resource_id
  scalable_dimension = aws_appautoscaling_target.this.scalable_dimension
  schedule           = each.value.schedule
  timezone           = var.schedule_time_zone

  scalable_target_action {
    min_capacity = each.value.tasks
    max_capacity = each.value.tasks
  }
}
