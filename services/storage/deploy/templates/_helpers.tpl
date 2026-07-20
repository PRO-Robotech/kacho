{{/*
Copyright (c) PRO-Robotech
SPDX-License-Identifier: BUSL-1.1
*/}}

{{/*
storage.fullname — the base resource name for every object in this chart.

By default it is "<Release.Name>-storage" (e.g. kacho-umbrella-storage), but a
`fullnameOverride` pins it to a stable value regardless of the release name. The
umbrella sets fullnameOverride=kacho-storage so the Service/Deployment/DNS name is
`kacho-storage` — the name every consumer dials (compute→storage
storageInternalAddr default kacho-storage.kacho.svc.cluster.local:9091, the
compute server-cert serverName.storage SAN, the geo/iam config defaults) and the
name deploy/Makefile reload-svc expects (DEPLOY_NAME=kacho-storage). Without the
override the Service renders as kacho-umbrella-storage and none of those resolve.
*/}}
{{- define "storage.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-storage" .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{/*
storage.labels — common labels for every object.
*/}}
{{- define "storage.labels" -}}
app: {{ include "storage.fullname" . }}
app.kubernetes.io/name: {{ include "storage.fullname" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}
