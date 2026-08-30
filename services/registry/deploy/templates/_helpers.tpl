{{/*
Helper templates for the kacho-registry sub-chart.
*/}}

{{/*
registry.fullname — the workload/base name. Driven by .Values.name (== Chart name
`registry`) so the Deployment, public Service and cert-SANs all agree.
*/}}
{{- define "registry.fullname" -}}
{{ .Values.name }}
{{- end -}}

{{/*
registry.image — kacho-registry container image reference (api-server + migrator
share one image). Prefers an immutable digest pin when .Values.image.digest is set
(repository@sha256:...); otherwise repository:tag.
*/}}
{{- define "registry.image" -}}
{{- $img := .Values.image -}}
{{- if $img.digest -}}
{{ $img.repository }}@{{ $img.digest }}
{{- else -}}
{{ $img.repository }}:{{ $img.tag }}
{{- end -}}
{{- end -}}

{{/*
registry.zotImage — zot OCI backend image reference (same digest-pin logic).
*/}}
{{- define "registry.zotImage" -}}
{{- $z := .Values.zot.image -}}
{{- if $z.digest -}}
{{ $z.repository }}@{{ $z.digest }}
{{- else -}}
{{ $z.repository }}:{{ $z.tag }}
{{- end -}}
{{- end -}}

{{/*
registry.zotAddr — the ZOT_ADDR the registry dials. Explicit .Values.zot.addr wins;
otherwise derived from the zot Service name + port (http://<serviceName>:<port>).
*/}}
{{- define "registry.zotAddr" -}}
{{- if .Values.zot.addr -}}
{{ .Values.zot.addr }}
{{- else -}}
http://{{ .Values.zot.serviceName }}:{{ .Values.zot.port }}
{{- end -}}
{{- end -}}

{{/*
registry.laneServiceAud — имя службы реестра у докерной полосы выдачи.

ОДНО ОБЪЯВЛЕНИЕ НА ОБЕ СТОРОНЫ ПОЛОСЫ. Это же имя обязан чеканить в `aud`
подписант iam: реестр называет его докер-клиенту в WWW-Authenticate, клиент
возвращает в `?service=`, iam сверяет с объявленным. Расхождение = отказ во
входе докера, поэтому источник у обеих сторон один —
`global.kacho.registry.serviceAud`, — и его читает ещё и подчарт kacho-iam.
`global` выбран не по вкусу: это единственное, что видно из ОБОИХ контекстов.

СОБСТВЕННАЯ РУЧКА `serviceAud` ОСТАЁТСЯ и означает ровно одно — «чарт поставлен
сам по себе, второй стороны рядом нет». Тогда связывать не с чем и отказывать
не за что.

ОБЪЯВИТЬ ОБЕ И РАЗНО — ОТКАЗ РЕНДЕРА, а не тихий выбор одной из них. Тихий
выбор и есть тот класс, из-за которого задача заведена: величина принята,
проигнорирована, и узнаёт об этом арендатор, а не оператор. Текст отказа
называет ОБЕ величины — оператор чинит это за минуту, если ему сказали, что
именно не сошлось.

НЕ ОБЪЯВИТЬ НИ ОДНОЙ — ЗАКОННО ЗДЕСЬ И МОЛЧИТ НАМЕРЕННО. Отказывает СТРАЖ
СТАРТА процесса (buildTokenVerifier: незаданный ожидаемый адресат означает
«принимаем любого»), и отказывает он со знанием посадки, которого у шаблона
нет. Подставить сюда имя вместо него — значит вернуть то самое второе
умолчание: чарт выбрал бы за оператора величину, которая у каждого кластера
своя, и выбрал бы её МОЛЧА.
*/}}
{{- define "registry.laneServiceAud" -}}
{{- $source := (((.Values.global).kacho).registry).serviceAud | default "" -}}
{{- $own := .Values.serviceAud | default "" -}}
{{- if and $source $own (ne $source $own) -}}
{{- fail (printf "registry: имя службы докерной полосы объявлено ДВАЖДЫ и по-разному — global.kacho.registry.serviceAud=%q против registry.serviceAud=%q. У полосы две стороны (реестр называет имя докер-клиенту, iam чеканит его в aud), и объявление у них одно: оставьте global.kacho.registry.serviceAud, а registry.serviceAud снимите — он для одиночной установки чарта, где второй стороны нет." $source $own) -}}
{{- end -}}
{{- $source | default $own -}}
{{- end -}}

{{/*
registry.dbSslMode — режим TLS до Postgres, ОДНИМ написанием.

Ключ звался в дереве двумя способами: `sslMode` у vpc/compute/iam/storage и
`sslmode` здесь. Оба означали одно, и оба лежали в одном профиле, поэтому
проверка «весь ли флот шифрован» одним `grep sslMode values.prod.yaml` молча
отвечала про половину флота и выглядела исчерпывающей. Это тот вид ошибки,
который не выдаёт себя ничем: сегодня обе половины стоят в `require`, и ответ
случайно верен.

Канон — `sslMode` (camelCase, как остальные ключи продукта). Это ПЕРЕИМЕНОВАНИЕ
ПОД ОХРАНОЙ, а не совместимость: канон объявлен умолчанием чарта, поэтому
профиль, задающий прежнее написание, даёт РАСХОЖДЕНИЕ и отказ рендера с обоими
значениями в тексте. Называть это «переходным приёмом» было бы неверно — приём
срабатывает лишь там, где канон не объявлен умолчанием.

Что механизм гарантирует на самом деле, и ради чего он здесь: молчаливого
отката к умолчанию чарта НЕ БУДЕТ. Для ключа, чьё умолчание у половины чартов —
`disable`, такой откат означал бы ОТКРЫТЫЙ ТЕКСТ до базы, то есть отказ, который
никто не заметит. Оператор вместо этого получает отказ выкатки, называющий оба
написания и канон; старое имя истекает громко и сразу, а не живёт вечной
совместимостью.

Заданы оба и различаются — ОТКАЗ РЕНДЕРА, а не выбор одного из двух: угадывать,
какое из двух написаний оператор считал действующим, значит решать за него
вопрос про шифрование. Пустое значение — тоже отказ.
*/}}
{{- define "registry.dbSslMode" -}}
{{- $canonRaw := dig "sslMode" "" . -}}
{{- $legacyRaw := dig "sslmode" "" . -}}
{{- $canon := "" -}}{{- if $canonRaw }}{{ $canon = printf "%v" $canonRaw }}{{ end -}}
{{- $legacy := "" -}}{{- if $legacyRaw }}{{ $legacy = printf "%v" $legacyRaw }}{{ end -}}
{{- if and (ne $canon "") (ne $legacy "") (ne $canon $legacy) -}}
{{- fail (printf "sslMode=%q и устаревшее sslmode=%q заданы одновременно и различаются: какое из двух написаний действует — решает оператор, а не шаблон. Оставьте одно, канон — sslMode" $canon $legacy) -}}
{{- end -}}
{{- $v := $canon -}}
{{- if eq $v "" }}{{ $v = $legacy }}{{ end -}}
{{- if eq $v "" -}}
{{- fail "sslMode не задан: канал к Postgres обязан быть объявлен явно (disable|require|verify-ca|verify-full); пустое значение молча дало бы открытый текст" -}}
{{- end -}}
{{- $v -}}
{{- end -}}
