resource "aws_lb" "this" {
  name               = "${var.name_prefix}-nlb"
  load_balancer_type = "network"
  internal           = false #tfsec:ignore:aws-elb-alb-not-public el navegador llega a la API por este NLB
  subnets            = var.public_subnet_ids

  security_groups = [var.security_group_id]

  enable_cross_zone_load_balancing = true

  tags = {
    Name = "${var.name_prefix}-nlb"
  }
}

resource "aws_lb_target_group" "api" {
  name        = "${var.name_prefix}-tg-api"
  target_type = "ip"
  protocol    = "TCP"
  port        = var.app_port
  vpc_id      = var.vpc_id

  deregistration_delay = 30

  health_check {
    protocol            = "HTTP"
    port                = "traffic-port"
    path                = var.health_check_path
    matcher             = "200"
    interval            = 15
    timeout             = 5
    healthy_threshold   = 2
    unhealthy_threshold = 2
  }

  tags = {
    Name = "${var.name_prefix}-tg-api"
  }
}

resource "aws_lb_listener" "http" {
  load_balancer_arn = aws_lb.this.arn
  protocol          = "TCP"
  port              = 80

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.api.arn
  }

  tags = {
    Name = "${var.name_prefix}-nlb-80"
  }
}

