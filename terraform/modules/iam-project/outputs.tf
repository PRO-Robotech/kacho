# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

output "project_id" {
  description = "Идентификатор проекта — им адресуются все ресурсы облака."
  value       = kacho_iam_project.this.id
}

output "group_ids" {
  description = "Идентификаторы групп по их именам."
  value       = { for k, g in kacho_iam_group.this : k => g.id }
}

output "service_account_ids" {
  description = "Идентификаторы служебных учёток по их именам."
  value       = { for k, s in kacho_iam_service_account.this : k => s.id }
}
