# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

output "service_account_id" {
  description = <<-EOT
    Идентификатор служебной учётки — им она называется в выдачах прав и в членстве в
    группах.

    Право выдаётся на этот идентификатор, а не на имя: имя косметическое и может
    меняться, а выдача обязана пережить переименование.
  EOT
  value       = kaname_service_account.this.id
}

output "key_ids" {
  description = <<-EOT
    Идентификаторы ключей по ключам входной карты.

    Ими ключ отзывается — и только ими: идентификатор подписи (`kid`) для отзыва не
    годится, это другое значение того же ключа.
  EOT
  value       = { for k, key in kaname_service_account_key.this : k => key.id }
}

output "client_ids" {
  description = <<-EOT
    Идентификатор клиента OAuth2 каждого ключа — им ключ представляется при обмене на
    токен.

    Выведен рядом с материалом намеренно: одного закрытого ключа для входа мало, и
    отсутствие второй половины обнаруживается уже в конвейере.
  EOT
  value       = { for k, key in kaname_service_account_key.this : k => key.client_id }
}

output "public_key_pems" {
  description = "Открытые ключи в PEM по ключам входной карты. Секрета не несут."
  value       = { for k, key in kaname_service_account_key.this : k => key.public_key_pem }
}

output "key_expires_at" {
  description = <<-EOT
    Когда истекает каждый ключ.

    Это единственное место, где видно, что смена ключа просрочена, — ради него срок и
    задаётся. Пусто здесь бывает только у ключа без срока, а такой получается в одном
    случае: `ttl_seconds = 0` встретил установку, где умолчание срока не настроено. Там,
    где оно настроено, ноль означает его, а не «никогда».
  EOT
  value       = { for k, key in kaname_service_account_key.this : k => key.expires_at }
}

output "private_key_pems" {
  description = <<-EOT
    Закрытые ключи в PEM по ключам входной карты — то, чем машина входит.

    ПРЕДУПРЕЖДЕНИЕ: материал лежит в файле состояния ОТКРЫТЫМ. Край отдаёт его один раз, в
    ответе на выпуск, и повторно не выдаёт никому — поэтому Terraform хранит его у себя.
    Пометка `sensitive` лишь скрывает значение в выводе команд; состояние она НЕ шифрует.
    Кто может прочитать состояние — тот входит как эта машина.

    Прочитать значение осознанно: `tofu output -json private_key_pems`. Печатать его в лог
    конвейера не нужно — лог переживает и ключ, и того, кто его выпускал.
  EOT
  value       = { for k, key in kaname_service_account_key.this : k => key.private_key_pem }
  sensitive   = true
}
