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

resource "kacho_registry_registry" "this" {
  project_id                    = var.project_id
  region_id                     = var.region_id
  name                          = var.name
  description                   = var.description
  labels                        = var.labels
  default_repository_visibility = var.default_repository_visibility
}

# Репозиторий ссылается на реестр ПО ИДЕНТИФИКАТОРУ: ссылка строит граф зависимостей, и
# снос идёт в обратном порядке — репозитории раньше реестра, который их держит. Край не
# удаляет непустой реестр, поэтому порядок здесь несущий, а не косметический.
resource "kacho_registry_repository" "this" {
  for_each = var.repositories

  registry_id = kacho_registry_registry.this.id
  repository  = each.key
  description = each.value.description
  visibility  = each.value.visibility
  labels      = var.labels
}
