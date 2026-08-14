# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

# Машина в сети, с ключом входа и правилом размещения.
#
# Показывает то, ради чего эти ресурсы разделены: ключ входа и группа размещения
# живут СВОИМ сроком жизни, а машина на них ссылается. Ключ можно отозвать, не
# трогая машину; группу — переиспользовать несколькими машинами.
terraform {
  required_providers {
    kacho = { source = "PRO-Robotech/kacho" }
  }
}

provider "kacho" {
  # endpoint и token читаются из KACHO_ENDPOINT и KACHO_TOKEN.
}

variable "project_id" { type = string }

variable "zone_id" {
  type    = string
  default = "ru-central1-a"
}

variable "boot_image_id" {
  type        = string
  description = "Образ, с которого машина загружается (ресурс хранилища)."
}

variable "public_key" {
  type        = string
  description = "ПУБЛИЧНАЯ половина ключа входа. Закрытая половина сюда не попадает никогда: состояние Terraform хранится открытым текстом."
}

# Размер называется ИМЕНЕМ каталога, а машине достаётся неизменяемый
# идентификатор. Вписать идентификатор прямо в конфигурацию значило бы сделать её
# непереносимой: у разных установок он разный.
# Поиск по ИМЕНИ делает списочный источник: имя в каталоге уникально, поэтому фильтр
# оставляет не больше одной записи. Одиночный источник читает по идентификатору — он
# для случая, когда идентификатор уже известен.
data "kacho_compute_machine_types" "small" {
  name = "std-v3-2"
}

locals {
  # `one()` — не украшение: он ПАДАЕТ, если записей больше одной, вместо того чтобы
  # молча взять первую. Имя уникально, значит расхождение здесь означало бы, что
  # изменился каталог, а не что надо выбрать любую.
  small_machine_type = one(data.kacho_compute_machine_types.small.machine_types)
}

module "vpc" {
  source = "../../modules/vpc-network"

  project_id       = var.project_id
  name             = "compute-example"
  ipv4_cidr_blocks = ["10.20.0.0/16"]

  subnets = {
    "compute-example-a" = {
      zone_id           = var.zone_id
      ipv4_cidr_primary = "10.20.1.0/24"
    }
  }
}

resource "kacho_compute_guest_access_key" "operator" {
  project_id = var.project_id
  name       = "operator"
  public_key = var.public_key
}

# SPREAD — разнести: отказ одного куска железа не должен унести всю группу.
# Числа доменов отказа здесь нет намеренно — оно описывало бы раскладку железа,
# а не намерение.
resource "kacho_compute_placement_group" "spread" {
  project_id = var.project_id
  name       = "compute-example-spread"
  strategy   = "SPREAD"

  # Ровно одна координата: zone_id ИЛИ region_id. Вид якоря выводится из неё.
  zone_id = var.zone_id
}

resource "kacho_compute_instance" "app" {
  project_id      = var.project_id
  name            = "compute-example-app"
  zone_id         = var.zone_id
  machine_type_id = local.small_machine_type.id

  # Род машины — первый различитель контракта, задаётся отдельно от спецификации.
  instance_kind = "VM"

  # Источник загрузки — ОДИН вложенный блок: тип называет владельца источника
  # (`storage.image` / `registry.image`), а не вид ресурса.
  boot_source = {
    type = "storage.image"
    id   = var.boot_image_id
  }

  # Интерфейс — элемент списка спецификаций: у машины их может быть несколько.
  network_interface_specs = [{
    subnet_id = module.vpc.subnet_ids["compute-example-a"]
  }]

  # Достижимость извне — ОСОЗНАННЫЙ выбор, умолчания у него нет: край требует либо
  # заказать внешний адрес, либо подтвердить, что снаружи до машины не достучаться.
  # Здесь подтверждаем: пример заводит машину во внутренней сети.
  acknowledge_unreachable = true

  guest_access_key_ids = [kacho_compute_guest_access_key.operator.id]
  placement_group_id   = kacho_compute_placement_group.spread.id

  lifecycle {
    # Каталог сообщает, где размер заказуем. Проверка стоит здесь, чтобы
    # несовпадение было видно НА ПЛАНЕ, а не отказом края на применении.
    precondition {
      condition     = contains(local.small_machine_type.available_zones, var.zone_id)
      error_message = "Тип машины ${local.small_machine_type.name} не заказуем в зоне ${var.zone_id}."
    }

    # Отпечаток вычисляет край. Сверьте его с тем, что видите у себя, — так
    # узнаете, что доехал именно ваш ключ.
    precondition {
      condition     = kacho_compute_guest_access_key.operator.fingerprint != ""
      error_message = "Край не сообщил отпечаток ключа входа."
    }
  }
}

output "instance_id" { value = kacho_compute_instance.app.id }
output "instance_fqdn" { value = kacho_compute_instance.app.fqdn }
output "machine_size" {
  value = {
    # Размерность лежит во вложенном блоке: у типа машины есть ОБЪЯВЛЕННЫЙ размер и
    # ДЕЙСТВУЮЩИЙ, и второй край считает сам. Плоские поля читали бы объявленное.
    vcpu       = local.small_machine_type.effective_resources.v_cpu
    memory_mib = local.small_machine_type.effective_resources.memory_mib
  }
}
