# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

# Наименьшая рабочая конфигурация: сеть с супернетом и две зональные подсети.
terraform {
  required_providers {
    kacho = { source = "PRO-Robotech/kacho" }
  }
}

provider "kacho" {
  # endpoint и token читаются из KACHO_ENDPOINT и KACHO_TOKEN.
  # Отключения проверки сертификата у провайдера нет: для внутреннего центра
  # сертификации укажите ca_bundle.
}

variable "project_id" { type = string }

module "vpc" {
  source = "../../modules/vpc-network"

  project_id       = var.project_id
  name             = "example"
  description      = "пример из репозитория провайдера"
  ipv4_cidr_blocks = ["10.10.0.0/16"]

  subnets = {
    "example-a" = {
      zone_id           = "ru-central1-a"
      ipv4_cidr_primary = "10.10.1.0/24"
    }
    "example-b" = {
      zone_id           = "ru-central1-b"
      ipv4_cidr_primary = "10.10.2.0/24"
    }
  }
}

output "network_id" { value = module.vpc.network_id }
output "subnet_ids" { value = module.vpc.subnet_ids }
