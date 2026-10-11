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

{{/*
─── ВНЕШНИЙ ВХОД КОНСОЛИ (publicFront, kacho#3024) ───────────────────────────

`ui.publicFrontOrigin` — происхождение консоли, ПРОВЕРЕННОЕ на то, что этот вход
способен его обслужить: схема `https`, без пути и порта, отличного от 443, а
хост — среди имён сертификата, когда сертификат заводит чарт. Величина одна —
`global.kacho.identity.appBaseURL`, её же служба доступа читает как адрес
консоли. Любое несоответствие — отказ рендера с именем ручки: вход, чьё
происхождение браузер не примет защищённым, вернул бы ровно тот отказ формы,
ради которого вход заведён.

`ui.publicFrontOriginHost` — хост того же происхождения после проверок схемы,
пути и порта, без проверки покрытия сертификатом: из него выводятся имена
сертификата (`namesFromOrigin`), и проверка покрытия сама на нём стоит.
*/}}
{{- define "ui.publicFrontOriginHost" -}}
{{- $id := ((.Values.global).kacho).identity | default dict -}}
{{- $origin := trimSuffix "/" ($id.appBaseURL | default "") -}}
{{- if not $origin -}}
{{- fail "uif.publicFront.enabled: global.kacho.identity.appBaseURL пуст — вход не знает происхождения, на которое переадресовывать и которое покрывать сертификатом" -}}
{{- end -}}
{{- $u := urlParse $origin -}}
{{- if ne $u.scheme "https" -}}
{{- fail (printf "uif.publicFront.enabled: происхождение консоли %q не по https — Secure-печенье формы с него браузер не хранит (kacho#3024)" $origin) -}}
{{- end -}}
{{- if or $u.path $u.query $u.fragment -}}
{{- fail (printf "uif.publicFront.enabled: происхождение консоли %q несёт путь или запрос — происхождение это схема, хост и порт" $origin) -}}
{{- end -}}
{{- $host := $u.host -}}
{{- if regexMatch ":[0-9]+$" $host -}}
{{- $port := regexFind "[0-9]+$" $host -}}
{{- if and (ne $port "443") (ne (include "ui.publicFrontServiceType" .) "ClusterIP") -}}
{{- fail (printf "uif.publicFront.enabled: происхождение консоли %q называет порт, отличный от 443, а вход публикует TLS на 443 (порт проброса допустим только у входа, который площадка не публикует: publicFront.service.type=ClusterIP)" $origin) -}}
{{- end -}}
{{- $host = trimSuffix (printf ":%s" $port) $host -}}
{{- end -}}
{{- $host -}}
{{- end -}}

{{/*
`ui.publicFrontCertNames` — имена сертификата входа: объявленные плюс, при
`namesFromOrigin`, хост происхождения консоли (IP-литерал — в ipAddresses,
имя — в dnsNames). Печатает YAML-словарь `{dns: [...], ip: [...]}`.
*/}}
{{- define "ui.publicFrontCertNames" -}}
{{- $c := .Values.publicFront.tls.certificate -}}
{{- $dns := $c.dnsNames | default list -}}
{{- $ip := $c.ipAddresses | default list -}}
{{- if $c.namesFromOrigin -}}
{{- $host := include "ui.publicFrontOriginHost" . -}}
{{- if or (regexMatch "^[0-9.]+$" $host) (hasPrefix "[" $host) -}}
{{- $ip = append $ip (trimSuffix "]" (trimPrefix "[" $host)) | uniq -}}
{{- else -}}
{{- $dns = append $dns $host | uniq -}}
{{- end -}}
{{- end -}}
{{- dict "dns" $dns "ip" $ip | toYaml -}}
{{- end -}}

{{- define "ui.publicFrontOrigin" -}}
{{- $pf := .Values.publicFront -}}
{{- $host := include "ui.publicFrontOriginHost" . -}}
{{- if $pf.tls.certificate.create -}}
{{- $n := include "ui.publicFrontCertNames" . | fromYaml -}}
{{- $names := concat ($n.dns | default list) ($n.ip | default list) -}}
{{- if not (has (trimSuffix "]" (trimPrefix "[" $host)) $names) -}}
{{- fail (printf "uif.publicFront.tls.certificate: хост происхождения консоли %q не назван ни в dnsNames, ни в ipAddresses — браузер отвергнет сертификат" $host) -}}
{{- end -}}
{{- end -}}
{{- printf "https://%s" $host -}}
{{- end -}}

{{/* Имена объектов поставщика входа (publicFront.acme). */}}
{{- define "ui.publicFrontIssuerName" -}}
{{- printf "%s-public-acme" (include "ui.hostName" .) -}}
{{- end -}}

{{- define "ui.publicFrontAcmeSolverName" -}}
{{- printf "%s-acme-solver" (include "ui.hostName" .) -}}
{{- end -}}

{{/* Порт пода решателя http01 — задан cert-manager, ручкой не является. */}}
{{- define "ui.acmeSolverPort" -}}8089{{- end -}}

{{/* `ui.publicFrontGuard` — отказ рендера на неполном блоке; печатает пусто. */}}
{{- define "ui.publicFrontGuard" -}}
{{- if not (and (gt (int .Values.tlsReload.intervalSeconds) 0) (le (int .Values.tlsReload.intervalSeconds) 3600)) -}}
{{- fail "uif.tlsReload.intervalSeconds вне (0, 3600] — продлённый сертификат раздача не перечитала бы вовремя" -}}
{{- end -}}
{{- $pf := .Values.publicFront -}}
{{- if $pf.enabled -}}
{{- if not $pf.tls.secretName -}}
{{- fail "uif.publicFront.tls.secretName пуст — вход без сертификата TLS не завершает" -}}
{{- end -}}
{{- if $pf.tls.certificate.create -}}
{{- if not (or $pf.tls.certificate.dnsNames $pf.tls.certificate.ipAddresses $pf.tls.certificate.namesFromOrigin) -}}
{{- fail "uif.publicFront.tls.certificate: ни dnsNames, ни ipAddresses, ни namesFromOrigin не объявлены — сертификату нечего удостоверять" -}}
{{- end -}}
{{- end -}}
{{- $acme := $pf.acme | default dict -}}
{{- if $acme.enabled -}}
{{- if not $pf.tls.certificate.create -}}
{{- fail "uif.publicFront.acme.enabled: поставщик из релиза выписывает лист, который заводит чарт, — tls.certificate.create обязан быть true" -}}
{{- end -}}
{{- if $pf.tls.certificate.issuerRef.name -}}
{{- fail "uif.publicFront.acme.enabled вместе с tls.certificate.issuerRef.name — у листа входа два поставщика; поставщик из релиза исключает внешний" -}}
{{- end -}}
{{- if not (hasPrefix "https://" ($acme.server | default "")) -}}
{{- fail "uif.publicFront.acme.server: каталог ACME обязан быть объявлен адресом по https" -}}
{{- end -}}
{{- else if and $pf.tls.certificate.create (not $pf.tls.certificate.issuerRef.name) -}}
{{- fail "uif.publicFront.tls.certificate.issuerRef.name пуст — сертификат некому выписать (либо acme.enabled: true — поставщик из релиза, либо certificate.create: false и секрет заводит оператор)" -}}
{{- end -}}
{{- if eq (int $pf.httpsPort) (int $pf.redirectPort) -}}
{{- fail "uif.publicFront: httpsPort и redirectPort совпадают" -}}
{{- end -}}
{{- if or (eq (int $pf.httpsPort) (int (include "ui.hostPort" .))) (eq (int $pf.redirectPort) (int (include "ui.hostPort" .))) -}}
{{- fail "uif.publicFront: httpsPort/redirectPort совпадает с внутренним портом раздачи" -}}
{{- end -}}
{{- $_ := include "ui.publicFrontOrigin" . -}}
{{- $_ := include "ui.publicFrontProxyProtocol" . -}}
{{- end -}}
{{- end -}}

{{- /*
ui.publicFrontProxyProtocol — адрес клиента от балансировщика площадки заголовком
PROXY (kacho#3115); печатает "true", когда приём включён, иначе пусто.

Действует ТОЛЬКО у входа, который публикует площадка (`service.type:
LoadBalancer`): вход, который пробрасывает к себе пользующийся (`ClusterIP`,
стенд проб), заголовка не получает, и слушатель, ждущий его, отверг бы каждое
соединение.

Включённый приём требует вместе, и рендер отказывает на любой недостающей:
  - `trustedFrom` — звено балансировки, от которого заголовку верят; пусто
    значило бы «верить никому» молча (адрес — снова балансировщик), а запись
    шире одного адреса (/32, /128) — верить и соседям звена: порт https раздачи
    доступен подам и узлам кластера, и каждый хост в доверенной подсети заявил
    бы чужой источник. Заглушку слоя площадки (адрес документации) судит
    общий страж слоя — templates/site-layer-guard.yaml умбреллы, а не этот;
  - `serviceAnnotations` — просьба к балансировщику слать заголовок. Слушатель,
    ждущий заголовка, без отправителя принял бы первую строку клиента за адрес;
    поэтому просьба объявлена здесь же, а не рядом в `service.annotations`, —
    одно включение, а не два;
  - ни один ключ `serviceAnnotations` не переобъявлен в `service.annotations`
    другим значением: иначе одно из двух мест молча перебило бы другое, и
    значение «не слать заголовок» при включённом приёме дало бы клиенту самому
    написать строку PROXY с любым адресом.

Выключенный приём у входа LoadBalancer требует ПРИЧИНЫ (`disabledBecause`):
пустая строка — это «забыли», и проверка не отличила бы её от «здесь намеренно
нет». Причина выходит аннотацией Service, её читает рендер-гейт
(deploy/console_public_front_render_test.go, п. 7).
*/}}
{{- define "ui.publicFrontProxyProtocol" -}}
{{- $pf := .Values.publicFront -}}
{{- $pp := (($pf.clientAddress | default dict).proxyProtocol) | default dict -}}
{{- if and $pf.enabled (not $pp.enabled) (eq (include "ui.publicFrontServiceType" .) "LoadBalancer") (not (trim (toString ($pp.disabledBecause | default "")))) -}}
{{- fail "uif.publicFront.clientAddress.proxyProtocol: приём заголовка PROXY выключен у входа LoadBalancer без причины (disabledBecause пуст) — за балансировщиком, соединяющимся со своего адреса, все клиенты стали бы одним источником (kacho#3115). Включите приём либо назовите причину" -}}
{{- end -}}
{{- if and $pf.enabled $pp.enabled (eq (include "ui.publicFrontServiceType" .) "LoadBalancer") -}}
{{- $from := $pp.trustedFrom | default list -}}
{{- if not $from -}}
{{- fail "uif.publicFront.clientAddress.proxyProtocol.trustedFrom пуст при включённом приёме заголовка PROXY — звено балансировки не названо, и адрес клиента снова был бы адресом балансировщика (kacho#3115)" -}}
{{- end -}}
{{- range $from -}}
{{- $e := toString . -}}
{{- $ok := false -}}
{{- if regexMatch `^([0-9]{1,3}\.){3}[0-9]{1,3}(/32)?$` $e -}}{{- $ok = true -}}{{- end -}}
{{- if regexMatch `^[0-9a-fA-F:]*:[0-9a-fA-F:]*(/128)?$` $e -}}{{- $ok = true -}}{{- end -}}
{{- if not $ok -}}
{{- fail (printf "uif.publicFront.clientAddress.proxyProtocol.trustedFrom: запись %q шире одного адреса звена балансировки (/32, /128) либо не адрес — хост кластера, дошедший до порта https, заявил бы чужой источник (kacho#3115)" $e) -}}
{{- end -}}
{{- end -}}
{{- if not $pp.serviceAnnotations -}}
{{- fail "uif.publicFront.clientAddress.proxyProtocol.serviceAnnotations пуст при включённом приёме заголовка PROXY — балансировщик площадки не попрошен слать заголовок, и слушатель принял бы первую строку клиента за адрес (kacho#3115)" -}}
{{- end -}}
{{- $own := $pf.service.annotations | default dict -}}
{{- range $k, $v := $pp.serviceAnnotations -}}
{{- if and (hasKey $own $k) (ne (toString (get $own $k)) (toString $v)) -}}
{{- fail (printf "uif.publicFront: аннотация %q объявлена дважды разными значениями — %q в clientAddress.proxyProtocol.serviceAnnotations и %q в service.annotations; просьба к балансировщику слать заголовок PROXY объявляется ОДНИМ местом (kacho#3115)" $k (toString $v) (toString (get $own $k))) -}}
{{- end -}}
{{- end -}}
true
{{- end -}}
{{- end -}}

{{- /*
ui.publicFrontServiceType — кто публикует вход (values.yaml, `publicFront.service.type`).
Закрытый перечень: значение вне него — отказ рендера, а не тихий LoadBalancer.
*/}}
{{- define "ui.publicFrontServiceType" -}}
{{- $t := .Values.publicFront.service.type | default "LoadBalancer" -}}
{{- if not (has $t (list "LoadBalancer" "ClusterIP")) -}}
{{- fail (printf "uif.publicFront.service.type=%q: допустимы LoadBalancer (вход публикует площадка) и ClusterIP (вход пробрасывает пользующийся)" $t) -}}
{{- end -}}
{{- $t -}}
{{- end -}}
{{- define "ui.publicFrontServiceName" -}}
{{- .Values.publicFront.service.name | default (printf "%s-public" (include "ui.hostName" .)) -}}
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

{{- /*
ЗВЕНО ФРОНТА К КРАЮ (kacho#3028). Раздача — звено фронта края: адрес клиента
край принимает от неё, только если она предъявила лист якоря звеньев с именем
звена (gateway/internal/linktls). Поэтому каждая полоса к краю идёт по TLS на
внешний слушатель края и несёт клиентский лист; лист края проверяется его
удостоверяющим центром и именем (имя — хост адреса края из
`host.upstreams.apiGateway`, одна величина на адрес и на имя).
*/ -}}
{{- define "ui.edgeLinkDir" -}}/etc/console-edge-link{{- end -}}
{{- define "ui.edgeCADir" -}}/etc/console-edge-ca{{- end -}}
{{- define "ui.edgeServerName" -}}
{{- $up := required "host.upstreams.apiGateway обязателен" .Values.host.upstreams.apiGateway -}}
{{- $host := regexReplaceAll ":[0-9]+$" $up "" -}}
{{- if eq $host $up -}}
{{- fail (printf "host.upstreams.apiGateway=%q без порта: полоса к краю идёт на его TLS-слушатель, порт обязателен" $up) -}}
{{- end -}}
{{- $host -}}
{{- end -}}
{{- define "ui.edgeLinkTLS" -}}
proxy_ssl_certificate {{ include "ui.edgeLinkDir" . }}/tls.crt;
proxy_ssl_certificate_key {{ include "ui.edgeLinkDir" . }}/tls.key;
proxy_ssl_trusted_certificate {{ include "ui.edgeCADir" . }}/ca.crt;
proxy_ssl_verify on;
proxy_ssl_verify_depth 2;
proxy_ssl_name {{ include "ui.edgeServerName" . }};
proxy_ssl_server_name on;
proxy_ssl_protocols TLSv1.2 TLSv1.3;
proxy_ssl_session_reuse on;
{{- end -}}
