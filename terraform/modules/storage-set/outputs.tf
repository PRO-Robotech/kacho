# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

output "volume_ids" {
  description = "Идентификаторы томов по ключам карты `volumes`."
  value       = { for k, v in kacho_storage_volume.this : k => v.id }
}

output "snapshot_ids" {
  description = <<-EOT
    Идентификаторы снимков по ключам карты `snapshots`.

    Выведены наружу не только для отчётности: том, выращенный из снимка ЭТОГО набора,
    внутри одного экземпляра модуля не заводится by construction (ссылка тома на снимок
    замкнула бы граф блоков). Такой том создаётся вторым экземпляром модуля, и вот
    идентификатор, который он получит на вход как `source_snapshot_id`.
  EOT
  value       = { for k, s in kacho_storage_snapshot.this : k => s.id }
}

output "image_ids" {
  description = "Идентификаторы образов по ключам карты `images`. Годятся как `source_image_id` тома в ЛЮБОЙ зоне региона `region_id`."
  value       = { for k, i in kacho_storage_image.this : k => i.id }
}

output "volumes" {
  description = <<-EOT
    Тома целиком: идентификатор, зона и состояние.

    Зона выведена рядом с идентификатором намеренно: она — тот вход правила когерентности,
    который вызывающему придётся сверять с регионом образа самому, и держать его в голове
    после apply не на чем.
  EOT
  value = {
    for k, v in kacho_storage_volume.this : k => {
      id      = v.id
      zone_id = v.zone_id
      status  = v.status
    }
  }
}

output "snapshots" {
  description = <<-EOT
    Снимки целиком: идентификатор, разрешённый идентификатор тома-источника и состояние.

    Состояние выведено намеренно: снимок существует раньше, чем становится пригоден как
    источник, и `status` — единственное место, где эта разница видна без обращения к краю.
  EOT
  value = {
    for k, s in kacho_storage_snapshot.this : k => {
      id               = s.id
      source_volume_id = s.source_volume_id
      status           = s.status
    }
  }
}

output "images" {
  description = <<-EOT
    Образы целиком: идентификатор, регион и состояние.

    Регион повторён у каждого образа, хотя задан одной переменной: выход читают там, где
    входа модуля не видно, а без региона идентификатор образа не говорит, в какой зоне из
    него можно поднять том.
  EOT
  value = {
    for k, i in kacho_storage_image.this : k => {
      id        = i.id
      region_id = i.region_id
      status    = i.status
    }
  }
}
