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
Помощник адреса слушателя хуков ПЕРЕЕХАЛ в _kratos-identity.tpl под именем
`kacho.identity.hooksAuthority`.

Причина не косметическая: адрес входит в содержимое настроек службы личности, а
это содержимое обязано вычисляться ДВАЖДЫ — в контексте нашего подчарта (карта
настроек) и в контексте подчарта провайдера (отпечаток содержимого в шаблоне
пода, без которого правка карты не перекатывает под). Прежняя редакция читала
`.Values.kratos.config.hooks` и `.Values.service.internal.hooksHttpPort` —
значения НАШЕГО подчарта, которых во втором контексте нет.

Копии здесь не оставлено намеренно: две реализации одного адреса разошлись бы
молча, и отпечаток перестал бы соответствовать содержимому.
*/}}

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

На посадке `own` запрос памяти ВЫВОДИТСЯ из предела (kacho#2727). Предел — третье
число бюджета полосы входа паролем (ёмкость × память одной проверки на потолке
формата + резерв); страж полосы сверяет бюджет с пределом среды и доказывает
право занять его целиком. Запрос, живущий отдельно, выбирал бы узел по числу, не
связанному с бюджетом ничем, и правка предела его не двигала бы. Поэтому на
`own` значение `resources.requests.memory` не читается: запрос памяти равен
пределу. Предела нет — запрос не трогается: страж полосы тогда откажет в старте
сам, «предел памяти средой не наложен», и этот случай судит
deploy/own_lane_memory_budget_test.go.

Класс обслуживания пода на `own` остаётся Burstable — решением, а не умолчанием:
процессор полосе не бюджетирован (запрос меньше предела), а инициализирующий
контейнер ресурсов не объявляет. По памяти под защищён: kubelet вытесняет
первыми поды, чьё потребление превышает запрос, а запрос здесь равен пределу.
Довод и решение держит deploy/own_lane_memory_request_test.go
(`ownLaneQoSDecision`).

Под `external` полоса не поднимается, и ресурсы уходят как объявлены: там
запрос памяти ниже предела — размен, названный в боевом профиле.
*/}}
{{- define "kaname.containerResources" -}}
{{- $res := deepCopy (.Values.resources | default dict) -}}
{{- if eq (toString .Values.config.authn.identityProvider) "own" -}}
{{- $limit := dig "limits" "memory" "" $res -}}
{{- if $limit -}}
{{- $requests := dig "requests" (dict) $res -}}
{{- $_ := set $requests "memory" $limit -}}
{{- $_ := set $res "requests" $requests -}}
{{- end -}}
{{- end -}}
{{- toYaml $res -}}
{{- end -}}

{{/*
kaname.hooksLaneRaised — ПОДНИМАЕТ ЛИ ПРОЦЕСС СЛУШАТЕЛЬ ВЕБХУКОВ ПОСТАВЩИКА
ЛИЧНОСТИ при посадке этого профиля (kacho#2871, служба — kaname#360). Отдаёт
`true` либо пусто.

ЗЕРКАЛО ПРЕДИКАТА ПРОЦЕССА, а не своё решение: служба снимает слушатель ровно
ОДНИМ объявленным значением — `own` (`AuthNConfig.HasExternalIdentityProvider`,
«не own»). Поэтому здесь «не own», а не «== external»: незаявленная посадка
слушатель СОХРАНЯЕТ, и чарт, сузивший условие, снял бы порт там, где процесс
дверь поднимает.

Читателей два — порт пода и порт внутреннего Service, — и порознь они разошлись
бы молча. Согласие с процессом на каждом стенде держит
`deploy/kaname_hooks_port_follows_posture_test.go`.
*/}}
{{- define "kaname.hooksLaneRaised" -}}
{{- $authn := (.Values.config | default dict).authn | default dict -}}
{{- if ne (toString ($authn.identityProvider | default "")) "own" -}}true{{- end -}}
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
`kaname.identity.webauthnRpId` и `kaname.identity.consoleOrigin` (_kratos-identity.tpl),
общих с настройками службы личности:

  · `rpId`          — имя доверяющей стороны ключей доступа;
  · `consoleOrigin` — происхождение консоли;
  · `loginURL`      — адрес экрана входа консоли в письмах нашего отправителя:
    `/login` в корне её происхождения (ui-future/shared/src/pages/auth/
    ceremony-addresses.ts). Служба берёт из него происхождение для адреса экрана
    подтверждения в письме (`<происхождение>/verification`). Литерала адреса в
    профиле нет — он был бы вторым местом о происхождении консоли.

Адрес входа — не одна из трёх величин, которые решение Р23 объявляет однажды для
обоих отправителей (узел, отправитель, удостоверение): поставщику личности он не
нужен. Поэтому он выводится из узла личности, а не из узла полосы; гейт питания
полосы (deploy/identity_mail_lane_feeds_both_senders_test.go, граница его предмета
названа в шапке) его не судит, судит исход рендера —
deploy/address_gate_stand_render_test.go.

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
kaname.mailCount — СЧЁТ ПОЧТОВОЙ РУЧКИ целым числом либо отказ рендера
(kacho#2915, Д64; та же форма, что `kaname-svc.mailCount` собственного чарта
службы).

Вход — список: путь ручки в значениях (для текста отказа) и величина. Голый
`int64` превращает слово в НОЛЬ, а у части счётов ноль законен («приглашений не
слать»): опечатка («unlimited», «off») доехала бы до стража законным с виду
числом и молча сменила бы смысл ручки. Дробное число — та же беда: `int64`
отбросил бы дробь. Границы здесь НЕ судятся — их судит страж старта службы по
таблице границ, второе место о тех же числах разошлось бы с ней.
*/}}
{{- define "kaname.mailCount" -}}
{{- $key := index . 0 -}}
{{- $v := index . 1 -}}
{{- if not (or (kindIs "float64" $v) (kindIs "int64" $v) (kindIs "int" $v)) -}}
{{- fail (printf "kaname: `%s` = %v — ожидается целое число; слова «без ограничения» у почтовых ручек нет (приёмка NTF-2 службы, Р8)" $key $v) -}}
{{- end -}}
{{- if ne (float64 $v) (floor (float64 $v)) -}}
{{- fail (printf "kaname: `%s` = %v — ожидается целое число, а не дробное (приёмка NTF-2 службы, Р8)" $key $v) -}}
{{- end -}}
{{- int64 $v -}}
{{- end -}}

{{/*
ФАЙЛЫ КЛЮЧЕЙ ПОЧТОВОЙ ПОЛОСЫ — ИМЕНА ПРИНАДЛЕЖАТ ШАБЛОНУ (kacho#2915, Д64).

Ключи объекта Secret и имена файлов в томе — одни и те же два слова; их читают
ДВА места (проекция тома в deployment.yaml и пути настроек в configmap.yaml), и
каждое берёт их отсюда — второе написание разошлось бы с первым молча, а отказ
пришёл бы в кластере, на чтении файла.

`kaname.mailKeys.secretName` — имя объявленного объекта либо отказ рендера.
Имя из одних пробелов — то же «не названо»: непустая строка истинна, и `if not`
пропустил бы её, а кластер отверг бы том с таким именем уже при установке — без
имени ключа. Судится обрезанное значение. Объект чарт не создаёт: на стендах его
заводит посев (deploy/scripts/dev-prod-secrets.sh), на площадке — оператор.
*/}}
{{- define "kaname.mailKeys.windowFile" -}}mail-window.key{{- end -}}
{{- define "kaname.mailKeys.deviceFile" -}}device-label.key{{- end -}}
{{- define "kaname.mailKeys.secretName" -}}
{{- $mk := .Values.config.authn.secrets | default dict -}}
{{- $name := trim (toString ($mk.secretName | default "")) -}}
{{- if not $name -}}
{{- fail "kaname: config.authn.secrets.secretName не назван (пусто либо одни пробелы) — имя объекта Secret с ключами почтовой полосы `mail-window.key` (k_window) и `device-label.key` (k_device), не короче 32 байт каждый. Служба без этих файлов не стартует ни на какой посадке; на стенде объект заводит deploy/scripts/dev-prod-secrets.sh, на площадке — оператор" -}}
{{- end -}}
{{- $name -}}
{{- end -}}
