# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

terraform {
  required_version = ">= 1.6"
  required_providers {
    kacho = {
      source = "PRO-Robotech/kacho"
    }
  }
}

locals {
  http_probe = var.health_check.http == null ? null : {
    port           = var.health_check.http.port
    path           = var.health_check.http.path
    expected_codes = var.health_check.http.expected_codes
    host           = var.health_check.http.host
    headers        = var.health_check.http.headers
  }
  https_probe = var.health_check.https == null ? null : {
    port           = var.health_check.https.port
    path           = var.health_check.https.path
    expected_codes = var.health_check.https.expected_codes
    host           = var.health_check.https.host
    headers        = var.health_check.https.headers
  }
}

resource "kacho_nlb_target_group" "this" {
  project_id = var.project_id
  region_id  = var.region_id
  name       = "${var.name}-tg"
  labels     = var.labels
  port       = var.backend_port

  deregistration_delay = var.deregistration_delay

  # Значения собираются ПОЛЕ ЗА ПОЛЕМ, а не передаются переменной целиком.
  #
  # Тип переменной модуля фиксирован её объявлением, а тип атрибута схемы включает и
  # вычисляемые поля (`effective_port`), которых во входной переменной нет и быть не
  # должно. Передача переменной целиком даёт «Value Conversion Error» — отказ, чей текст
  # не называет ни поля, ни причины. Литерал же Terraform приводит к типу схемы сам,
  # дополняя недостающее null.
  health_check = {
    interval            = var.health_check.interval
    timeout             = var.health_check.timeout
    healthy_threshold   = var.health_check.healthy_threshold
    unhealthy_threshold = var.health_check.unhealthy_threshold
    tcp                 = var.health_check.tcp == null ? null : { port = var.health_check.tcp.port }
    http                = var.health_check.http == null ? null : local.http_probe
    https               = var.health_check.https == null ? null : local.https_probe
    grpc = var.health_check.grpc == null ? null : {
      port         = var.health_check.grpc.port
      service_name = var.health_check.grpc.service_name
    }
  }

  targets = [
    for t in var.targets : {
      instance_id = t.instance_id
      nic_id      = t.nic_id
      ip_ref      = t.ip_ref == null ? null : { subnet_id = t.ip_ref.subnet_id, address = t.ip_ref.address }
      external_ip = t.external_ip == null ? null : { address = t.external_ip.address, zone_id = t.external_ip.zone_id }
      weight      = t.weight
    }
  ]
}

resource "kacho_nlb_load_balancer" "this" {
  project_id = var.project_id
  region_id  = var.region_id
  name       = var.name
  labels     = var.labels
  placement  = var.placement

  v4_source = var.v4_source == null ? null : {
    subnet_id = var.v4_source.subnet_id, address_id = var.v4_source.address_id
  }
  v6_source = var.v6_source == null ? null : {
    subnet_id = var.v6_source.subnet_id, address_id = var.v6_source.address_id
  }
}

# Слушатели ссылаются на балансировщик и группу ПО ИДЕНТИФИКАТОРУ, а не по имени: ссылка
# строит граф зависимостей, и снос пойдёт в обратном порядке — слушатели раньше группы,
# которую они держат. Порядок здесь несущий: край не удаляет группу, на которую ссылаются.
resource "kacho_nlb_listener" "this" {
  for_each = var.listeners

  load_balancer_id = kacho_nlb_load_balancer.this.id
  name             = "${var.name}-${each.key}"
  labels           = var.labels
  protocol         = each.value.protocol
  port             = each.value.port
  target_group_id  = kacho_nlb_target_group.this.id
}
