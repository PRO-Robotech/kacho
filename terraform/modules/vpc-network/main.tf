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

resource "kacho_vpc_network" "this" {
  project_id       = var.project_id
  name             = var.name
  description      = var.description
  labels           = var.labels
  ipv4_cidr_blocks = var.ipv4_cidr_blocks
}

# Подсети ссылаются на сеть по идентификатору, а не по имени: ссылка строит граф
# зависимостей, и уничтожение пойдёт в обратном порядке — подсети раньше сети. Полагаться
# на «неверный порядок даст ошибку» здесь нельзя: часть связей при неверном порядке не
# падает, а молча обнуляется.
resource "kacho_vpc_subnet" "this" {
  for_each = var.subnets

  project_id        = var.project_id
  network_id        = kacho_vpc_network.this.id
  name              = each.key
  description       = each.value.description
  labels            = each.value.labels
  zone_id           = each.value.zone_id
  region_id         = each.value.region_id
  ipv4_cidr_primary = each.value.ipv4_cidr_primary
  route_table_id    = each.value.route_table_id
}
