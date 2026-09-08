# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

output "project_id" {
  description = "Идентификатор проекта — им адресуются все ресурсы облака."
  value       = kaname_project.this.id
}

output "group_ids" {
  description = "Идентификаторы групп по их именам."
  value       = { for k, g in kaname_group.this : k => g.id }
}

output "service_account_ids" {
  description = "Идентификаторы служебных учёток по их именам."
  value       = { for k, s in kaname_service_account.this : k => s.id }
}

# Почты в выходах нет намеренно. В состоянии она и так лежит, но выход — это то, что
# печатается в терминал и уезжает в чужие модули; персональные данные так расходятся
# дальше, чем кто-либо решал. Наружу отдаётся то, чем человека адресуют: идентификатор.
output "invitation_ids" {
  description = <<-EOT
    Идентификаторы строк членства по псевдонимам приглашённых.

    Ими человек и адресуется в выдаче прав (`iam-access`, `subject_grants`): адресация на
    платформе идёт по неизменяемому идентификатору, а не по почте — почта меняется, а
    выданное право обязано пережить её смену.
  EOT
  value       = { for k, i in kaname_user_invitation.this : k => i.id }
}

output "invitation_statuses" {
  description = <<-EOT
    Состояние каждого приглашения: `PENDING` — отправлено, вход не состоялся; `ACTIVE` —
    человек вошёл; `BLOCKED` — участие запрещено.

    Двумя из трёх значений модуль НЕ управляет: `PENDING → ACTIVE` переводит вход человека
    через провайдера личности, и `apply` этого не ждёт — значение здесь снимок на момент
    последнего чтения.

    А `BLOCKED` — ровно то, чем управляет вход `blocked`: у края запрет входа и состояние
    строки суть ОДИН факт (`blocked` читается как «состояние равно `BLOCKED`»), поэтому
    `blocked = true` виден здесь как `BLOCKED`, а не отдельным полем. Сказано потому, что
    обратное — «состояние меняется только человеком» — читается естественно и неверно.
  EOT
  value       = { for k, i in kaname_user_invitation.this : k => i.invite_status }
}
