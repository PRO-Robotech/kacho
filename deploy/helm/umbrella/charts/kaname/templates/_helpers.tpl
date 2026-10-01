{{/*
Helper templates для kaname sub-chart.
*/}}

{{/* Полное имя релиза kaname — по соглашению просто .Values.name. */}}
{{- define "kaname.fullname" -}}
{{- default "kaname" .Values.name -}}
{{- end -}}

{{/* Common labels. */}}
{{- define "kaname.labels" -}}
app: {{ include "kaname.fullname" . }}
app.kubernetes.io/name: {{ include "kaname.fullname" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/* Selector labels (без managed-by/instance — иначе подменится при reload). */}}
{{- define "kaname.selectorLabels" -}}
app: {{ include "kaname.fullname" . }}
{{- end -}}

{{/*
Container image reference. Prefers an immutable digest pin (repository@sha256:...)
when .Values.image.digest is set; otherwise falls back to repository:tag.
*/}}
{{- define "kaname.image" -}}
{{- if .Values.image.digest -}}
{{ .Values.image.repository }}@{{ .Values.image.digest }}
{{- else -}}
{{ .Values.image.repository }}:{{ .Values.image.tag }}
{{- end -}}
{{- end -}}

{{/*
kaname.refuseRetiredKnobs — СНЯТАЯ РУЧКА, МЕНЯВШАЯ ПОВЕДЕНИЕ, ОТВЕРГАЕТСЯ ВСЛУХ
(kacho#2818).

Служба с kaname#363 держит одну посадку личности и ключ посадки отвергает при
любом значении. Ручка подчарта, которая его передавала, снята вместе с
читателем. Профиль, всё ещё объявляющий её, получил бы значение, которого шаблон
не читает, и читался бы как решение о посадке, которого больше никто не
принимает. Поэтому — отказ рендера с именем ручки, та же дисциплина, что у
стража снятых ключей в самой службе (retired_settings.go) и у снятых ручек
транспорта в deployment.yaml.

Вторая строка — `platform.iam.saKey.bindDpop` (kacho#2949). Выдача ключей
служебных учёток на пине службы не несёт требования связанного с отправителем
токена: токен-эндпоинт, где ключ обменивается, доказательства владения не
принимает, и токен, который он чеканит, `cnf` не несёт. Читатель у ручки один —
страж старта службы, и он отказывает в старте, когда ручка включена при
включённом токен-эндпоинте (`config.authn.clientToken.enabled`); без
токен-эндпоинта ключи служебных учёток не выдаются вовсе. Ручка не меняла
выдачи ни на одном контуре, а меняла исход старта — потому отказ рендера, а не
молчаливое снятие: профиль, всё ещё её объявляющий, иначе выглядел бы решением
о связанных машинных токенах, которого никто не исполняет. Отказ держит
секция 6 deploy/tests/helm/machine-credential-posture-test.sh; что она краснеет
без этой строки, доказывает ось 7 machine-credential-posture-inject.sh.

ПОЧЕМУ В ПЕРЕЧНЕ ДВЕ СТРОКИ, ХОТЯ СНЯТО БОЛЬШЕ — довод тот же, что у службы.
Изменением первой строки ушли ручки полосы хуков, зеркала набора ключей и
административной дороги прежнего поставщика. Их значение не меняло старта и
прежде: служба их уже не читала, и отказ на них заставил бы каждую установку
менять профиль ради того, что поведения не меняло. Профили дерева их не несут —
это держит перепись читателей ключей профилей
(internal/repohygiene/profileknobreader_test.go).

Судится ПРИСУТСТВИЕ ключа, а не значение: пустая строка — тоже объявление.
Строка сюда добавляется тем изменением, которое снимает ручку, чьё значение
меняло поведение.
*/}}
{{- define "kaname.refuseRetiredKnobs" -}}
{{- range $knob := list
  (list "config.authn.identityProvider" "посадка личности у службы одна (kaname#363), и ключ посадки служба отвергает при любом значении")
  (list "platform.iam.saKey.bindDpop" "выдача ключей служебных учёток не несёт требования связанного токена: токен-эндпоинт не принимает доказательства владения, и при включённом config.authn.clientToken.enabled служба с этой ручкой отказывает в старте (kacho#2949)")
}}
{{- $node := $.Values -}}
{{- $found := true -}}
{{- range $seg := splitList "." (index $knob 0) -}}
{{- if and $found (kindIs "map" $node) (hasKey $node $seg) -}}
{{- $node = index $node $seg -}}
{{- else -}}
{{- $found = false -}}
{{- end -}}
{{- end -}}
{{- if $found -}}
{{- fail (printf "kaname: ручка %s снята — %s. Уберите её из профиля: принятая и непрочитанная, она выглядела бы решением" (index $knob 0) (index $knob 1)) -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
kaname.laneServiceAud — адресат, которому докерная полоса выдачи чеканит `aud`.

ВЫВОДИТСЯ ИЗ ОБЪЯВЛЕННОЙ СТОРОНЫ РЕЕСТРА, а не объявляется вторым умолчанием.
Реестр называет это имя докер-клиенту в WWW-Authenticate (`KACHO_REGISTRY_SERVICE_AUD`),
клиент возвращает его в `?service=`, а наш подписант сверяет с объявленным и
отвергает всё прочее. Пока сверки не было, расхождение двух объявлений было
НЕВИДИМО — клиент echo-ит услышанное, реестр это и ждёт; со сверкой то же
расхождение означает отказ во входе на каждом запросе арендатора.

Источник один — `global.kacho.registry.serviceAud`; его же читает подчарт
registry (`registry.laneServiceAud`). `global` выбран потому, что это
единственное, что видно из ОБОИХ контекстов сабчартов, — тот же довод, что у
формирующих значений службы личности.

СОБСТВЕННАЯ РУЧКА `config.apiServer.registryToken.service` ОСТАЁТСЯ и означает
«чарт поставлен сам по себе». Объявить обе и по-разному — отказ рендера с
обеими величинами, а не тихий выбор одной: тихий выбор и есть класс, из-за
которого задача заведена.

ПУСТО ЗАКОННО и молчит здесь намеренно: слушателя полосы может не быть вовсе.
Если он есть, а имени нет — отказывает СТРАЖ СТАРТА процесса, который про
слушателя знает, а шаблон не знает.
*/}}
{{- define "kaname.laneServiceAud" -}}
{{- $source := (((.Values.global).kacho).registry).serviceAud | default "" -}}
{{- $own := (((.Values.config).apiServer).registryToken).service | default "" -}}
{{- if and $source $own (ne $source $own) -}}
{{- fail (printf "kaname: адресат докерной полосы объявлен ДВАЖДЫ и по-разному — global.kacho.registry.serviceAud=%q против kaname.config.apiServer.registryToken.service=%q. У полосы две стороны (реестр называет имя докер-клиенту, iam чеканит его в aud), и объявление у них одно: оставьте global.kacho.registry.serviceAud, а config.apiServer.registryToken.service снимите — он для одиночной установки чарта, где второй стороны нет." $source $own) -}}
{{- end -}}
{{- $source | default $own -}}
{{- end -}}

{{/*
kaname.authMode — посадка безопасности, ОДНИМ адресом.

Ручку звали в дереве шестью разными адресами и двумя именами: `authn.mode`,
`config.authn.mode`, `auth.mode`, `config.mode`, `config.authMode`, `authMode`.
Ночью первый вопрос оператора — «в какой посадке работает этот кластер», и
задать его одной командой по одному ключу было НЕЛЬЗЯ: надо знать семь адресов.
Ужесточить флот одним `--set` тоже нельзя — путей столько же, сколько сервисов.

Канон — `authMode` в корне значений сервиса (camelCase, как прочие ключи
продукта; так уже было объявлено у registry и geo).

Это ПЕРЕИМЕНОВАНИЕ ПОД ОХРАНОЙ, а не совместимость: канон объявлен умолчанием
чарта, поэтому профиль, задающий прежний адрес, даёт расхождение и отказ
рендера с обоими значениями в тексте. Гарантия здесь одна и ради неё всё:
молчаливого отката к умолчанию чарта НЕ БУДЕТ. Для ручки, задающей посадку
безопасности, такой откат означал бы стенд, который называет себя одним, а
работает другим, — и заметить это можно только по последствиям.
*/}}
{{- define "kaname.authMode" -}}
{{- $vals := .Values | toYaml | fromYaml -}}
{{- $canonRaw := .Values.authMode -}}
{{- $legacyRaw := "" -}}
{{- $cur := $vals -}}
{{- if kindIs "map" $cur }}{{ $cur = dig "config" (dict) $cur }}{{ else }}{{ $cur = dict }}{{ end -}}
{{- if kindIs "map" $cur }}{{ $cur = dig "authn" (dict) $cur }}{{ else }}{{ $cur = dict }}{{ end -}}
{{- if kindIs "map" $cur }}{{ $legacyRaw = dig "mode" "" $cur }}{{ end -}}
{{- $canon := "" -}}{{- if $canonRaw }}{{ $canon = printf "%v" $canonRaw }}{{ end -}}
{{- $legacy := "" -}}{{- if $legacyRaw }}{{ $legacy = printf "%v" $legacyRaw }}{{ end -}}
{{- if and (ne $canon "") (ne $legacy "") (ne $canon $legacy) -}}
{{- fail (printf "authMode=%q и прежний адрес config.authn.mode=%q заданы одновременно и различаются: какой из двух адресов задаёт посадку — решает оператор, а не шаблон. Оставьте один, канон — authMode" $canon $legacy) -}}
{{- end -}}
{{- if ne $canon "" }}{{ $canon }}{{ else }}{{ $legacy }}{{ end -}}
{{- end -}}

{{/*
kaname.trustDomain — ДОМЕН ДОВЕРИЯ установки, ОДНИМ написанием.

Домен читают ДВЕ стороны: чеканка сертификата (`certificate.yaml`) и процесс,
которому сертификаты предъявляют (`authn.trust-domain` в его файле настроек).
Пока каждая брала умолчание своей рукой, расходились они МОЛЧА: сертификат
выпускался под новым доменом, принимающая сторона знала прежний, и законный
отправитель переставал опознаваться — отказом, неотличимым от вызова без
личности.

Умолчание живёт ЗДЕСЬ и только здесь. В коде его нет намеренно: непустое
умолчание сборки увело бы установку, забывшую назвать свой домен, в чужой, и
контроль выглядел бы включённым.
*/}}
{{- define "kaname.trustDomain" -}}
{{- $sp := (.Values.mtls | default dict).spiffe | default dict -}}
{{- $sp.trustDomain | default "kacho.cloud" -}}
{{- end -}}

{{/*
kaname.containerResources — ресурсы контейнера службы.

Запрос памяти ВЫВОДИТСЯ из предела (kacho#2727). Предел — третье
число бюджета полосы входа паролем (ёмкость × память одной проверки на потолке
формата + резерв); страж полосы сверяет бюджет с пределом среды и доказывает
право занять его целиком. Запрос, живущий отдельно, выбирал бы узел по числу, не
связанному с бюджетом ничем, и правка предела его не двигала бы. Поэтому
значение `resources.requests.memory` не читается: запрос памяти равен
пределу. Предела нет — запрос не трогается: страж полосы тогда откажет в старте
сам, «предел памяти средой не наложен», и этот случай судит
deploy/own_lane_memory_budget_test.go.

Класс обслуживания пода остаётся Burstable — решением, а не умолчанием:
процессор полосе не бюджетирован (запрос меньше предела), а инициализирующий
контейнер ресурсов не объявляет. По памяти под защищён: kubelet вытесняет
первыми поды, чьё потребление превышает запрос, а запрос здесь равен пределу.
Довод и решение держит deploy/own_lane_memory_request_test.go
(`ownLaneQoSDecision`).

Прежде вывод стоял под условием посадки: под второй посадкой полосы не было,
и ресурсы уходили как объявлены. Посадка у службы одна (kaname#363), поэтому
вывод безусловен (kacho#2818).
*/}}
{{- define "kaname.containerResources" -}}
{{- $res := deepCopy (.Values.resources | default dict) -}}
{{- $limit := dig "limits" "memory" "" $res -}}
{{- if $limit -}}
{{- $requests := dig "requests" (dict) $res -}}
{{- $_ := set $requests "memory" $limit -}}
{{- $_ := set $res "requests" $requests -}}
{{- end -}}
{{- toYaml $res -}}
{{- end -}}

{{/*
ЯКОРЬ ПОЧТОВОГО УЗЛА У НАШЕГО ОТПРАВИТЕЛЯ (kacho#2901, приёмка F6b, F6b-56).

Аргумент — узел почтовой полосы (`smtp` узла личности раздела `global`); источник
якоря в нём — `trustAnchorSecret` (секрет и ключ). Объявлен целиком ⇒ рабочий
объект монтирует из секрета ОДИН этот ключ под именем `ca.crt` в каталог ниже, а
настройка процесса получает путь к нему (`invite-mail.ca-bundle-file`). Каталог и
имя файла принадлежат шаблону, а не профилю: два читателя одного пути — том пода
и настройка — берут его отсюда, и разойтись им нечем.

Объявлен наполовину ⇒ якоря нет, и это не молчание: половину пары отвергает страж
рендера зонта (templates/identity-mail-lane-guard.yaml, (7)), называя ключ.
*/}}
{{- define "kaname.mailAnchor.dir" -}}/etc/kaname-mail-anchor{{- end -}}
{{- define "kaname.mailAnchor.file" -}}{{ include "kaname.mailAnchor.dir" . }}/ca.crt{{- end -}}
{{- define "kaname.mailAnchor.declared" -}}
{{- $a := (. | default dict).trustAnchorSecret | default dict -}}
{{- if and (ne (trim (toString ($a.name | default ""))) "") (ne (trim (toString ($a.key | default ""))) "") -}}true{{- end -}}
{{- end -}}

{{/*
ВЕЛИЧИНЫ, ВЫВОДИМЫЕ ИЗ УЗЛА ЛИЧНОСТИ, — ОДНИМ ЧИТАТЕЛЕМ (kacho#2905, kacho#2901).

Аргумент — `(list $ "<величина>")`. Узел личности раздела `global` подчарт читает
для своих выводимых величин здесь, в одном месте; сами выражения — у шаблонов
`kaname.identity.webauthnRpId` и `kaname.identity.consoleOrigin` ниже:

  · `rpId`          — имя доверяющей стороны ключей доступа;
  · `consoleOrigin` — происхождение консоли;
  · `loginURL`      — адрес экрана входа консоли в письмах нашего отправителя:
    `/login` в корне её происхождения (ui-future/shared/src/pages/auth/
    ceremony-addresses.ts). Служба берёт из него происхождение для адреса экрана
    подтверждения в письме (`<происхождение>/verification`). Литерала адреса в
    профиле нет — он был бы вторым местом о происхождении консоли.

Адрес входа выводится из узла личности, а не из узла почтовой полосы: у полосы
три величины (узел, отправитель, удостоверение), и адреса консоли среди них нет.
Его судит исход рендера — deploy/address_gate_stand_render_test.go.

Незнакомое имя величины — отказ рендера, а не пустая строка.
*/}}
{{- define "kaname.identity.derived" -}}
{{- $root := index . 0 -}}
{{- $what := index . 1 -}}
{{- $id := $root.Values.global.kacho.identity | default dict -}}
{{- if eq $what "rpId" -}}
{{- include "kaname.identity.webauthnRpId" $id -}}
{{- else if eq $what "consoleOrigin" -}}
{{- include "kaname.identity.consoleOrigin" $id -}}
{{- else if eq $what "loginURL" -}}
{{- printf "%s/login" (trimSuffix "/" (include "kaname.identity.consoleOrigin" $id)) -}}
{{- else -}}
{{- fail (printf "kaname.identity.derived: величина %q не выводится из узла личности" (toString $what)) -}}
{{- end -}}
{{- end -}}

{{/*
ИМЯ ДОВЕРЯЮЩЕЙ СТОРОНЫ КЛЮЧЕЙ ДОСТУПА И ПРОИСХОЖДЕНИЕ КОНСОЛИ — ОДНО ВЫРАЖЕНИЕ.

Аргумент — узел `identity` раздела `global`, а не корень. Ключ привязан браузером
к имени доверяющей стороны, поэтому имя обязано быть одним у всякого, кто его
называет; прежде выражение стояло у каждого читателя своим, и профиль повторял
имя литералом рядом с общим ключом — второе место об одном предмете (kacho#2905).

Пусто ⇒ `domain` (у имени) и `https://<appSubdomain>.<domain>` (у происхождения):
идиома «не объявлено ⇒ вывести». Сходимость имени и происхождения у настроек
службы держит deploy/own_ceilings_and_access_keys_umbrella_test.go.
*/}}
{{- define "kaname.identity.webauthnRpId" -}}
{{- .webauthnRpId | default .domain -}}
{{- end -}}

{{- define "kaname.identity.consoleOrigin" -}}
{{- .appBaseURL | default (printf "https://%s.%s" .appSubdomain .domain) -}}
{{- end -}}
