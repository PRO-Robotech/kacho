# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# ПЕРЕЕЗД СОСТОЯНИЯ: типы доступа сменили имя, ресурсы — нет.
#
# Имя типа — адрес записи в состоянии оператора. Переименование без объявления переезда даёт
# не ошибку, а удаление живой записи и создание новой: прежняя запись становится сиротой, а
# ресурс у края остаётся, и второй apply заводит дубль.
#
# Объявления ниже — ПОЛОВИНА пары. Вторая живёт в провайдере: исполнитель шлёт запрос
# переезда целевому типу, и тип, не объявивший поддержки, роняет план словами «The target
# resource implementation does not include move resource state support». Поэтому блок
# `moved` без объявления на стороне провайдера не работает — замерено на OpenTofu 1.12.5.
#
# Локальное имя провайдера здесь `kaname` ОДНО, и второго не нужно: префикс `kacho_` в
# адресе источника провайдера не требует — исполнитель не резолвит провайдера для адреса,
# которого в настройке уже нет (замерено). Объявить второе имя того же источника можно, но
# оно ничего не даёт и стоит предупреждения «A provider can only be required once»; хуже
# того, локальное имя, оставшееся без своего блока `provider`, получает НЕнастроенный
# провайдер, и план отказывает «requires explicit configuration».

moved {
  from = kacho_iam_project.this
  to   = kaname_project.this
}

moved {
  from = kacho_iam_group.this
  to   = kaname_group.this
}

moved {
  from = kacho_iam_service_account.this
  to   = kaname_service_account.this
}

moved {
  from = kacho_iam_user_invitation.this
  to   = kaname_user_invitation.this
}
