# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

terraform {
  # 1.6 достаточно: межпеременных проверок здесь нет — все validation смотрят
  # только на собственную переменную. Поднимать требование до 1.9, как в
  # storage-set, значило бы просить у вызывающего версию, которой модуль не
  # пользуется.
  required_version = ">= 1.6"

  required_providers {
    kacho = {
      source = "PRO-Robotech/kacho"
    }
  }
}

# Каждый атрибут присваивается ОТДЕЛЬНО, а не переданным целиком объектом: у
# объекта переменной есть необязательные поля, и присваивание целиком отдало бы
# провайдеру null там, где вызывающий поле не называл, — то есть «снять значение»
# вместо «не трогать».
resource "kacho_compute_instance" "this" {
  for_each = var.machines

  project_id  = var.project_id
  name        = each.value.name
  description = each.value.description
  labels      = var.labels

  # Зона машины и зона подсети её интерфейса обязаны совпадать — это инвариант
  # платформы, а не соглашение модуля. Здесь он выполняется по построению: зона
  # одна на весь набор, и подсеть выбирает вызывающий под неё.
  zone_id         = var.zone_id
  machine_type_id = each.value.machine_type_id

  boot_source_type = each.value.boot_source_type
  boot_source_id   = each.value.boot_source_id

  subnet_id          = each.value.subnet_id
  security_group_ids = each.value.security_group_ids

  # Группа: либо созданная ЭТИМ модулем (по ключу), либо чужая (по идентификатору).
  # Ровно одно из двух — это проверяет переменная; здесь остаётся разрешение.
  placement_group_id = (
    each.value.placement_group_key != null
    ? kacho_compute_placement_group.this[each.value.placement_group_key].id
    : each.value.placement_group_id
  )

  # Ключи входа СКЛАДЫВАЮТСЯ, а не выбираются: набор ключей — это множество, и у
  # машины законно бывают и свои, и заведённые снаружи. Пустой результат остаётся
  # пустым списком, а не null: null означал бы «не сказано», и провайдер не стал бы
  # трогать набор вовсе.
  guest_access_key_ids = concat(
    [for k in coalesce(each.value.guest_access_key_keys, []) : kacho_compute_guest_access_key.this[k].id],
    coalesce(each.value.guest_access_key_ids, []),
  )

  service_account_id = each.value.service_account_id
}

# Ключи входа и группы размещения заводятся ЗДЕСЬ ЖЕ, а не отдельным модулем.
#
# Причина не в удобстве: у обоих срок жизни привязан к набору машин, и разнеся их
# по модулям, мы отдали бы вызывающему порядок уничтожения. Он ошибётся молча —
# группа, снятая раньше машин, оставит их без правила размещения, а ключ, снятый
# позже машины, переживёт её без пользы. Внутри одного модуля порядок выводит сам
# Terraform из ссылок.
resource "kacho_compute_guest_access_key" "this" {
  for_each = var.guest_access_keys

  project_id = var.project_id
  name       = each.value.name
  # ТОЛЬКО публичная половина. Закрытая не принимается ни модулем, ни провайдером:
  # состояние Terraform хранится открытым текстом.
  public_key = each.value.public_key
  labels     = var.labels
}

resource "kacho_compute_placement_group" "this" {
  for_each = var.placement_groups

  project_id  = var.project_id
  name        = each.value.name
  description = each.value.description
  strategy    = each.value.strategy
  labels      = var.labels

  # Ровно одна координата якоря. Модуль НЕ выводит её из своей зоны: региональная
  # группа зоне-независима, и подставив сюда var.zone_id, мы сделали бы вторую
  # форму невыразимой.
  zone_id   = each.value.zone_id
  region_id = each.value.region_id
}
