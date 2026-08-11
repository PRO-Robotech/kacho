{{- define "ui.hostName" -}}
{{- default .Values.name .Values.host.name -}}
{{- end -}}

{{- define "ui.dashboardName" -}}
{{- default "ui-dashboard" .Values.dashboard.name -}}
{{- end -}}

{{- define "ui.vpcName" -}}
{{- default "ui-vpc" .Values.vpc.name -}}
{{- end -}}

{{- define "ui.iamName" -}}
{{- default "ui-iam" .Values.iam.name -}}
{{- end -}}

{{- define "ui.systemName" -}}
{{- default "ui-system" .Values.system.name -}}
{{- end -}}

{{- define "ui.hostImage" -}}
{{- default .Values.image .Values.host.image -}}
{{- end -}}

{{- define "ui.hostImagePullPolicy" -}}
{{- default .Values.imagePullPolicy .Values.host.imagePullPolicy -}}
{{- end -}}

{{- define "ui.dashboardImage" -}}
{{- if .Values.dashboard.image -}}
{{- .Values.dashboard.image -}}
{{- else -}}
{{- $hostImage := include "ui.hostImage" . -}}
{{- if contains "host" $hostImage -}}
{{- replace "host" "dashboard" $hostImage -}}
{{- else -}}
{{- "kacho-ui-future-dashboard:dev" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "ui.dashboardImagePullPolicy" -}}
{{- default (include "ui.hostImagePullPolicy" .) .Values.dashboard.imagePullPolicy -}}
{{- end -}}

{{- define "ui.vpcImage" -}}
{{- if .Values.vpc.image -}}
{{- .Values.vpc.image -}}
{{- else -}}
{{- $hostImage := include "ui.hostImage" . -}}
{{- if contains "host" $hostImage -}}
{{- replace "host" "vpc" $hostImage -}}
{{- else -}}
{{- "kacho-ui-future-vpc:dev" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "ui.vpcImagePullPolicy" -}}
{{- default (include "ui.hostImagePullPolicy" .) .Values.vpc.imagePullPolicy -}}
{{- end -}}

{{- define "ui.iamImage" -}}
{{- if .Values.iam.image -}}
{{- .Values.iam.image -}}
{{- else -}}
{{- $hostImage := include "ui.hostImage" . -}}
{{- if contains "host" $hostImage -}}
{{- replace "host" "iam" $hostImage -}}
{{- else -}}
{{- "kacho-ui-future-iam:dev" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "ui.iamImagePullPolicy" -}}
{{- default (include "ui.hostImagePullPolicy" .) .Values.iam.imagePullPolicy -}}
{{- end -}}

{{- define "ui.systemImage" -}}
{{- if .Values.system.image -}}
{{- .Values.system.image -}}
{{- else -}}
{{- $hostImage := include "ui.hostImage" . -}}
{{- if contains "host" $hostImage -}}
{{- replace "host" "system" $hostImage -}}
{{- else -}}
{{- "kacho-ui-future-system:dev" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "ui.systemImagePullPolicy" -}}
{{- default (include "ui.hostImagePullPolicy" .) .Values.system.imagePullPolicy -}}
{{- end -}}

{{- define "ui.dashboardUpstream" -}}
{{- if .Values.host.upstreams.dashboard -}}
{{- .Values.host.upstreams.dashboard -}}
{{- else -}}
{{- printf "%s.%s.svc.cluster.local:%v" (include "ui.dashboardName" .) .Release.Namespace .Values.dashboard.port -}}
{{- end -}}
{{- end -}}

{{- define "ui.vpcUpstream" -}}
{{- if .Values.host.upstreams.vpc -}}
{{- .Values.host.upstreams.vpc -}}
{{- else -}}
{{- printf "%s.%s.svc.cluster.local:%v" (include "ui.vpcName" .) .Release.Namespace .Values.vpc.port -}}
{{- end -}}
{{- end -}}

{{- define "ui.iamUpstream" -}}
{{- if .Values.host.upstreams.iam -}}
{{- .Values.host.upstreams.iam -}}
{{- else -}}
{{- printf "%s.%s.svc.cluster.local:%v" (include "ui.iamName" .) .Release.Namespace .Values.iam.port -}}
{{- end -}}
{{- end -}}

{{- define "ui.systemUpstream" -}}
{{- if .Values.host.upstreams.system -}}
{{- .Values.host.upstreams.system -}}
{{- else -}}
{{- printf "%s.%s.svc.cluster.local:%v" (include "ui.systemName" .) .Release.Namespace .Values.system.port -}}
{{- end -}}
{{- end -}}

{{- define "ui.hostPort" -}}
{{- default .Values.port .Values.host.port -}}
{{- end -}}

{{- define "ui.hostReplicas" -}}
{{- default .Values.replicas .Values.host.replicas -}}
{{- end -}}

{{- define "ui.securityHeaders" -}}
{{- if .Values.security.enabled }}
add_header X-Frame-Options "DENY" always;
add_header X-Content-Type-Options "nosniff" always;
add_header Referrer-Policy "no-referrer" always;
{{- if .Values.security.strictTransportSecurity }}
add_header Strict-Transport-Security "{{ .Values.security.strictTransportSecurity }}" always;
{{- end }}
{{- if .Values.security.contentSecurityPolicy }}
add_header Content-Security-Policy "{{ .Values.security.contentSecurityPolicy }}" always;
{{- end }}
{{- end }}
{{- end -}}

{{- define "ui.labels" -}}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/instance: {{ .Release.Name }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}
{{- end -}}

{{- define "ui.hostSelectorLabels" -}}
app: {{ include "ui.hostName" . }}
app.kubernetes.io/name: {{ include "ui.hostName" . }}
app.kubernetes.io/component: host
{{- end -}}

{{- define "ui.dashboardSelectorLabels" -}}
app: {{ include "ui.dashboardName" . }}
app.kubernetes.io/name: {{ include "ui.dashboardName" . }}
app.kubernetes.io/component: dashboard-remote
{{- end -}}

{{- define "ui.vpcSelectorLabels" -}}
app: {{ include "ui.vpcName" . }}
app.kubernetes.io/name: {{ include "ui.vpcName" . }}
app.kubernetes.io/component: vpc-remote
{{- end -}}

{{- define "ui.iamSelectorLabels" -}}
app: {{ include "ui.iamName" . }}
app.kubernetes.io/name: {{ include "ui.iamName" . }}
app.kubernetes.io/component: iam-remote
{{- end -}}

{{- define "ui.nlbName" -}}
{{- default "ui-nlb" .Values.nlb.name -}}
{{- end -}}

{{- define "ui.nlbImage" -}}
{{- if .Values.nlb.image -}}
{{- .Values.nlb.image -}}
{{- else -}}
{{- $hostImage := include "ui.hostImage" . -}}
{{- if contains "host" $hostImage -}}
{{- replace "host" "nlb" $hostImage -}}
{{- else -}}
{{- "kacho-ui-future-nlb:dev" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "ui.nlbImagePullPolicy" -}}
{{- default (include "ui.hostImagePullPolicy" .) .Values.nlb.imagePullPolicy -}}
{{- end -}}

{{- define "ui.nlbUpstream" -}}
{{- if .Values.host.upstreams.nlb -}}
{{- .Values.host.upstreams.nlb -}}
{{- else -}}
{{- printf "%s.%s.svc.cluster.local:%v" (include "ui.nlbName" .) .Release.Namespace .Values.nlb.port -}}
{{- end -}}
{{- end -}}

{{- define "ui.nlbSelectorLabels" -}}
app: {{ include "ui.nlbName" . }}
app.kubernetes.io/name: {{ include "ui.nlbName" . }}
app.kubernetes.io/component: nlb-remote
{{- end -}}

{{- define "ui.registryName" -}}
{{- default "ui-registry" .Values.registry.name -}}
{{- end -}}

{{- define "ui.registryImage" -}}
{{- if .Values.registry.image -}}
{{- .Values.registry.image -}}
{{- else -}}
{{- $hostImage := include "ui.hostImage" . -}}
{{- if contains "host" $hostImage -}}
{{- replace "host" "registry" $hostImage -}}
{{- else -}}
{{- "kacho-ui-future-registry:dev" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "ui.registryImagePullPolicy" -}}
{{- default (include "ui.hostImagePullPolicy" .) .Values.registry.imagePullPolicy -}}
{{- end -}}

{{- define "ui.registryUpstream" -}}
{{- if .Values.host.upstreams.registry -}}
{{- .Values.host.upstreams.registry -}}
{{- else -}}
{{- printf "%s.%s.svc.cluster.local:%v" (include "ui.registryName" .) .Release.Namespace .Values.registry.port -}}
{{- end -}}
{{- end -}}

{{- define "ui.registrySelectorLabels" -}}
app: {{ include "ui.registryName" . }}
app.kubernetes.io/name: {{ include "ui.registryName" . }}
app.kubernetes.io/component: registry-remote
{{- end -}}

{{- define "ui.systemSelectorLabels" -}}
app: {{ include "ui.systemName" . }}
app.kubernetes.io/name: {{ include "ui.systemName" . }}
app.kubernetes.io/component: system-remote
{{- end -}}

{{- define "ui.computeName" -}}
{{- default "ui-compute" .Values.compute.name -}}
{{- end -}}

{{- define "ui.computeImage" -}}
{{- if .Values.compute.image -}}
{{- .Values.compute.image -}}
{{- else -}}
{{- $hostImage := include "ui.hostImage" . -}}
{{- if contains "host" $hostImage -}}
{{- replace "host" "compute" $hostImage -}}
{{- else -}}
{{- "kacho-ui-future-compute:dev" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "ui.computeImagePullPolicy" -}}
{{- default (include "ui.hostImagePullPolicy" .) .Values.compute.imagePullPolicy -}}
{{- end -}}

{{- define "ui.computeUpstream" -}}
{{- if .Values.host.upstreams.compute -}}
{{- .Values.host.upstreams.compute -}}
{{- else -}}
{{- printf "%s.%s.svc.cluster.local:%v" (include "ui.computeName" .) .Release.Namespace .Values.compute.port -}}
{{- end -}}
{{- end -}}

{{- define "ui.computeSelectorLabels" -}}
app: {{ include "ui.computeName" . }}
app.kubernetes.io/name: {{ include "ui.computeName" . }}
app.kubernetes.io/component: compute-remote
{{- end -}}

{{- define "ui.storageName" -}}
{{- default "ui-storage" .Values.storage.name -}}
{{- end -}}

{{- define "ui.storageImage" -}}
{{- if .Values.storage.image -}}
{{- .Values.storage.image -}}
{{- else -}}
{{- $hostImage := include "ui.hostImage" . -}}
{{- if contains "host" $hostImage -}}
{{- replace "host" "storage" $hostImage -}}
{{- else -}}
{{- "kacho-ui-future-storage:dev" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "ui.storageImagePullPolicy" -}}
{{- default (include "ui.hostImagePullPolicy" .) .Values.storage.imagePullPolicy -}}
{{- end -}}

{{- define "ui.storageUpstream" -}}
{{- if .Values.host.upstreams.storage -}}
{{- .Values.host.upstreams.storage -}}
{{- else -}}
{{- printf "%s.%s.svc.cluster.local:%v" (include "ui.storageName" .) .Release.Namespace .Values.storage.port -}}
{{- end -}}
{{- end -}}

{{- define "ui.storageSelectorLabels" -}}
app: {{ include "ui.storageName" . }}
app.kubernetes.io/name: {{ include "ui.storageName" . }}
app.kubernetes.io/component: storage-remote
{{- end -}}

{{- define "ui.hostResources" -}}
{{- if .Values.host.resources }}
{{- toYaml .Values.host.resources }}
{{- else }}
{{- toYaml .Values.resources }}
{{- end }}
{{- end -}}
