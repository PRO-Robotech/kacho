{{/*
Helper templates для kacho-iam sub-chart.
*/}}

{{/* Полное имя релиза kacho-iam — по соглашению просто .Values.name. */}}
{{- define "kacho-iam.fullname" -}}
{{- default "kacho-iam" .Values.name -}}
{{- end -}}

{{/* Common labels. */}}
{{- define "kacho-iam.labels" -}}
app: {{ include "kacho-iam.fullname" . }}
app.kubernetes.io/name: {{ include "kacho-iam.fullname" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/* Selector labels (без managed-by/instance — иначе подменится при reload). */}}
{{- define "kacho-iam.selectorLabels" -}}
app: {{ include "kacho-iam.fullname" . }}
{{- end -}}

{{/*
Container image reference. Prefers an immutable digest pin (repository@sha256:...)
when .Values.image.digest is set; otherwise falls back to repository:tag.
*/}}
{{- define "kacho-iam.image" -}}
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
kacho-iam.laneServiceAud — адресат, которому докерная полоса выдачи чеканит `aud`.

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
{{- define "kacho-iam.laneServiceAud" -}}
{{- $source := (((.Values.global).kacho).registry).serviceAud | default "" -}}
{{- $own := (((.Values.config).apiServer).registryToken).service | default "" -}}
{{- if and $source $own (ne $source $own) -}}
{{- fail (printf "kacho-iam: адресат докерной полосы объявлен ДВАЖДЫ и по-разному — global.kacho.registry.serviceAud=%q против kacho-iam.config.apiServer.registryToken.service=%q. У полосы две стороны (реестр называет имя докер-клиенту, iam чеканит его в aud), и объявление у них одно: оставьте global.kacho.registry.serviceAud, а config.apiServer.registryToken.service снимите — он для одиночной установки чарта, где второй стороны нет." $source $own) -}}
{{- end -}}
{{- $source | default $own -}}
{{- end -}}
