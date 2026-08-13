# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

output "registry_id" {
  description = "Идентификатор реестра — он же сегмент пути загрузки."
  value       = kacho_registry_registry.this.id
}

output "pull_paths" {
  description = <<-EOT
    Путь загрузки каждого репозитория БЕЗ домена и тега: `<registryId>/<репозиторий>`.

    Домен задаётся установкой платформы и модулю не известен, поэтому он сюда не
    подставляется: выдуманное значение выглядело бы рабочим и вело бы в никуда.
  EOT
  value       = { for k, r in kacho_registry_repository.this : k => "${kacho_registry_registry.this.id}/${r.repository}" }
}

output "repository_visibility" {
  description = <<-EOT
    Фактическая видимость каждого репозитория — та, что вернул край.

    Выведена наружу намеренно: репозиторий, унаследовавший умолчание, отличается от
    заданного явно только здесь.
  EOT
  value       = { for k, r in kacho_registry_repository.this : k => r.visibility }
}
