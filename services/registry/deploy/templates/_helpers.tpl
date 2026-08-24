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
