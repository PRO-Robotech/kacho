# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

variable "scope" {
  description = <<-EOT
    Якорь модуля: уровень и объект, на котором ОПРЕДЕЛЕНЫ его роли и к которому привязаны
    его выдачи.

    Одна переменная на оба ресурса намеренно: роль, определённая на проекте, выдаётся только
    на своём проекте, и две переменные позволили бы задать расхождение, которое отвергнется
    только на apply.

    Сужение выдачи внутри якоря делается целью (`target`), а не вторым якорем. Роль,
    определённая выше (аккаунтная) или системная, выдаётся на этот якорь по идентификатору —
    полем `role_id`, и определять её здесь не требуется.

    `iam.cluster` недопустим: роли такого уровня край не создаёт вовсе (их сеет платформа
    миграциями), а выдача на кластер — это супер-доступ, который принимают отдельным
    решением, а не получают модулем удобства.
  EOT
  type = object({
    type = string
    id   = string
  })

  validation {
    condition     = contains(["iam.account", "iam.project"], var.scope.type)
    error_message = "scope.type принимает iam.account или iam.project в дотированной форме: короткое «account» и уровень iam.cluster не принимаются."
  }
}

variable "roles" {
  description = <<-EOT
    Роли, определяемые модулем: ключ — имя роли в пределах якоря, значение — описание и политика.

    Роль сама по себе не даёт ничего: право начинает действовать только выдачей. Имя
    косметическое — выдачи ссылаются на идентификатор роли, поэтому переименование ничего
    не ломает.

    Одно правило — однородная выдача: перечисленные глаголы над перечисленными типами РОВНО
    ОДНОГО модуля; роль на несколько модулей — это несколько правил. Сужение необязательно и
    задаётся либо перечнем идентификаторов (`resource_names`), либо отбором по меткам
    (`match_labels`); отсутствие сужения выражается ОПУСКАНИЕМ поля, а не пустым значением —
    край не отличает пустой перечень от отсутствующего и вернёт его отсутствующим.

    Перечни модулей, типов ресурсов и глаголов принадлежат КРАЮ и проверяются им: своя копия
    начала бы отвергать законную настройку в тот день, когда платформа заведёт новый тип, и
    виноватым выглядел бы край.
  EOT
  type = map(object({
    description = optional(string, "")
    rules = list(object({
      module         = string
      resources      = list(string)
      verbs          = list(string)
      resource_names = optional(list(string))
      match_labels   = optional(map(string))
    }))
  }))
  default = {}

  validation {
    condition     = alltrue([for _, r in var.roles : length(r.rules) > 0])
    error_message = "Политика роли пуста: rules не содержит ни одного правила. Роль без правил не выдаёт ничего, и край такую не заводит."
  }

  validation {
    condition = alltrue([
      for _, r in var.roles : alltrue([
        for rule in r.rules : !(rule.resource_names != null && rule.match_labels != null)
      ])
    ])
    error_message = "Правило сужено дважды: заданы и resource_names, и match_labels. Край принимает ровно одно сужение — перечнем идентификаторов ЛИБО отбором по меткам."
  }
}

variable "group_grants" {
  description = <<-EOT
    ОСНОВНОЙ вход — карта «группа → роль»: ключ это имя группы, значение — её идентификатор,
    выдаваемая роль и цель.

    Право, которое получает больше одного принципала ИЛИ чей состав может измениться, выдаётся
    ГРУППЕ, а люди и служебные учётки добавляются в неё. Цена видна на снятии: убрать одного из
    группы — одна запись членства; убрать одного из перечисления получателей — снятие выданных
    кортежей по всем объектам роли и вдобавок пересоздание самой привязки, потому что состав
    получателей у края неизменяем.

    Идентификатор группы — ПОЛЕ, а не ключ: ключи `for_each` обязаны быть известны на плане, а
    идентификатор приезжает от края. Ключ, взятый из идентификатора, сделал бы невозможным план
    на ещё не заведённой группе.

    Роль называется ровно одним способом: `role` — имя роли ИЗ ЭТОГО модуля, `role_id` — готовая
    роль (системная или определённая выше). Два написания одного и того же разошлись бы.

    У цели умолчания НЕТ намеренно: широчайшая выдача берётся только явным `all_in_scope = true`,
    чтобы её нельзя было получить по невнимательности; пообъектная — непустым `resources`.

    Опущенный `expires_at` означает БЕССРОЧНУЮ выдачу — это умолчание края, и оно названо
    здесь, а не оставлено читателю: право, задуманное «на время», иначе остаётся навсегда и
    молча. Общего срока модуль не подставляет намеренно: момент абсолютный, и один на всех
    он в один день истёк бы у каждой выдачи сразу. Право снимается снятием выдачи, а не
    ожиданием срока; заданный срок — дополнение к этому, а не замена.

    Та же пара полей (`expires_at`, `deletion_protection`) с тем же смыслом есть и у
    `subject_grants`.
  EOT
  type = map(object({
    group_id = string
    role     = optional(string)
    role_id  = optional(string)
    target = object({
      all_in_scope = optional(bool)
      resources    = optional(list(object({ type = string, id = string })))
    })
    expires_at          = optional(string)
    deletion_protection = optional(bool, false)
  }))
  default = {}

  # Правила ниже повторены у `subject_grants` дословно. Это не небрежность: проверка
  # переменной не видит ни locals, ни соседних переменных, поэтому общего места для них в
  # модуле нет. Правя одну, правьте обе — иначе исключение станет мягче основного входа.
  validation {
    condition     = alltrue([for _, g in var.group_grants : (g.role == null) != (g.role_id == null)])
    error_message = "В каждой выдаче задаётся ровно одно из role (роль этого модуля) и role_id (готовая роль)."
  }

  validation {
    condition = alltrue([
      for _, g in var.group_grants :
      (g.target.all_in_scope == true) != (g.target.resources != null && length(coalesce(g.target.resources, [])) > 0)
    ])
    error_message = "В target выбирается ровно одна ветвь: all_in_scope = true ЛИБО непустой resources. Умолчания у цели нет — широчайшая выдача берётся только явным словом."
  }

  validation {
    condition     = alltrue([for _, g in var.group_grants : g.target.all_in_scope != false])
    error_message = "target.all_in_scope = false не выражает ничего: ветвь цели выбирается ОПУСКАНИЕМ поля, а два способа сказать «не эта ветвь» разошлись бы между настройкой и состоянием."
  }
}

variable "subject_grants" {
  description = <<-EOT
    ИСКЛЮЧЕНИЕ — выдача НАПРЯМУЮ одному принципалу, минуя группу.

    Отдельная переменная, а не поле общей карты, намеренно: перечисление законно ровно в одном
    случае — получатель ОДИН и меняться не будет (служебная учётка модуля, чья личность и есть
    предмет выдачи). Форма входа сделана под этот случай и только под него:

    * получатель ровно один — перечислить нескольких здесь нечем, и «добавить второго» означает
      завести группу, а не дописать строку;
    * `justification` обязателен и непуст — он и есть осознанный шаг: в нём пишут, почему состав
      получателей не изменится. Все обоснования модуля видны разом в выходе
      `subject_grant_justifications`;
    * группа сюда не кладётся — для неё есть основной вход, а две дороги к одному предмету
      разошлись бы, и половина групповых выдач установки оказалась бы в карте исключений.
  EOT
  type = map(object({
    subject = object({
      type = string
      id   = string
    })
    justification = string
    role          = optional(string)
    role_id       = optional(string)
    target = object({
      all_in_scope = optional(bool)
      resources    = optional(list(object({ type = string, id = string })))
    })
    expires_at          = optional(string)
    deletion_protection = optional(bool, false)
  }))
  default = {}

  validation {
    condition = alltrue([
      for _, s in var.subject_grants :
      contains(["SUBJECT_TYPE_USER", "SUBJECT_TYPE_SERVICE_ACCOUNT"], s.subject.type)
    ])
    error_message = "subject.type принимает SUBJECT_TYPE_USER или SUBJECT_TYPE_SERVICE_ACCOUNT. Группе право выдаётся основным входом group_grants, а не здесь."
  }

  validation {
    condition     = alltrue([for _, s in var.subject_grants : trimspace(s.justification) != ""])
    error_message = "justification пуст: назовите причину, по которой получатель этого права один и не изменится. Право на многих или на меняющийся состав выдаётся группе через group_grants."
  }

  # Дословный повтор проверок group_grants — см. причину там же.
  validation {
    condition     = alltrue([for _, s in var.subject_grants : (s.role == null) != (s.role_id == null)])
    error_message = "В каждой выдаче задаётся ровно одно из role (роль этого модуля) и role_id (готовая роль)."
  }

  validation {
    condition = alltrue([
      for _, s in var.subject_grants :
      (s.target.all_in_scope == true) != (s.target.resources != null && length(coalesce(s.target.resources, [])) > 0)
    ])
    error_message = "В target выбирается ровно одна ветвь: all_in_scope = true ЛИБО непустой resources. Умолчания у цели нет — широчайшая выдача берётся только явным словом."
  }

  validation {
    condition     = alltrue([for _, s in var.subject_grants : s.target.all_in_scope != false])
    error_message = "target.all_in_scope = false не выражает ничего: ветвь цели выбирается ОПУСКАНИЕМ поля, а два способа сказать «не эта ветвь» разошлись бы между настройкой и состоянием."
  }
}

variable "labels" {
  description = "Метки, применяемые ко всем ресурсам модуля — и к ролям, и к выдачам."
  type        = map(string)
  default     = {}
}
