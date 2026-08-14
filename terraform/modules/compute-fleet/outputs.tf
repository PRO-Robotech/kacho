# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

output "instance_ids" {
  description = "Идентификаторы машин по их именам."
  value       = { for k, i in kacho_compute_instance.this : k => i.id }
}

output "instances" {
  description = <<-EOT
    Машина целиком: идентификатор, состояние и его причина.

    Причина состояния выведена наружу намеренно: машина в неработающем состоянии выглядит
    в плане так же, как рабочая, и отличить их можно только по ней.
  EOT
  value = {
    for k, i in kacho_compute_instance.this : k => {
      id            = i.id
      status        = i.status
      status_reason = i.status_reason
      fqdn          = i.fqdn
    }
  }
}
