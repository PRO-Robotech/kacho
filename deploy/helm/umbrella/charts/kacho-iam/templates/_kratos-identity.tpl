{{/*
Copyright (c) PRO-Robotech
SPDX-License-Identifier: BUSL-1.1

СОДЕРЖИМОЕ НАСТРОЕК СЛУЖБЫ ЛИЧНОСТИ — ОДНО ОБЪЯВЛЕНИЕ, ЧИТАЕМОЕ ИЗ ДВУХ КОНТЕКСТОВ.

═══════════════════════════════════════════════════════════════════════════════
ЗАЧЕМ ЭТОТ ФАЙЛ

Настройки приезжают в под ТОМОМ и читаются процессом ОДИН РАЗ — на старте.
Правка карты настроек не меняет шаблон пода, значит под НЕ ПЕРЕКАТЫВАЕТСЯ, и
процесс продолжает жить со старым содержимым. Лечится отпечатком содержимого в
шаблоне пода: меняется содержимое — меняется шаблон — под перекатывается.

Вычислить такой отпечаток В ЗНАЧЕНИЯХ ПРОФИЛЯ нельзя: профиль — статический YAML.
Вычислять его надо там, где рендерится шаблон пода, то есть в контексте САБЧАРТА
ПРОВАЙДЕРА. Из ручек провайдера через шаблонизацию (`tpl`) проходит РОВНО ОДНА —
`deployment.extraEnv`; `deployment.annotations` и `podMetadata.annotations` идут
прямым `toYaml`, и sha256 в них вычислить не из чего. Тот же вывод и та же
развилка, что у соседа-терминатора административного перехода (_hydra-admin-tls.tpl).

Отсюда требование к ЭТОМУ файлу: содержимое обязано вычисляться и в контексте
нашего подчарта (где рендерится карта настроек), и в контексте подчарта
провайдера (где рендерится шаблон пода). Общими для обоих являются только
`.Values.global` и `.Release` — поэтому формирующие значения живут в
`global.kacho.identity`, а не в значениях нашего подчарта.

ПОЧЕМУ ОДНО ОБЪЯВЛЕНИЕ, А НЕ ДВА. Если бы карта настроек рендерилась из одного
текста, а отпечаток считался по другому, они разошлись бы молча — и привязка
пода к содержимому стала бы ЛОЖНОЙ ровно тогда, когда она нужна: содержимое
изменилось, отпечаток нет, под не перекатился. Поэтому и карта, и отпечаток
зовут ОДИН И ТОТ ЖЕ именованный шаблон.

СХЕМА АДРЕСА ОБРАТНЫХ ВЫЗОВОВ ЧИТАЕТСЯ ЗДЕСЬ ЗНАЧЕНИЕМ, А НЕ КОНСТАНТОЙ, и
намеренно записана полным путём `.Values.global.kacho.identity.hooks.scheme`
у каждого места вызова: транспорт — решение профиля, и оно обязано быть видно
там, где принимается. Держит это deploy/identity_callback_transport_test.go.
*/}}

{{/*
Authority (узел:порт) слушателя хуков iam. Схема сюда НЕ входит — см. выше.

Умолчания здесь — КОНСТАНТЫ, и это не небрежность, а следствие двух контекстов:
прежде они выводились из значений НАШЕГО подчарта (его имени и порта его
внутреннего слушателя), а в контексте подчарта провайдера этих значений нет.
Профиль, ничего не объявивший, получает прежний адрес байт в байт — это
проверено сравнением рендеров.

Цена решения названа и закрыта: связь «порт полосы = порт слушателя»
разорвалась, поэтому её держит утверждение —
deploy/identity_global_defaults_agree_test.go, проба про умолчания адреса.
Сменив порт слушателя или имя подчарта, вы получите красное, а не молчание.
*/}}
{{- define "kacho.identity.hooksAuthority" -}}
{{- $h := ((.Values.global.kacho).identity).hooks | default dict -}}
{{- $host := ($h.host | default (printf "kacho-iam-internal.%s.svc" .Release.Namespace)) -}}
{{- $port := ($h.port | default 9092) -}}
{{- printf "%s:%v" $host $port -}}
{{- end -}}

{{/* Тело настроек службы личности (`kratos.yaml`). */}}
{{- define "kacho.identity.configYaml" -}}
version: v1.3.1

# ─── Serve endpoints ────────────────────────────────────────────────
serve:
  public:
    base_url: https://{{ .Values.global.kacho.identity.kratosSubdomain }}.{{ .Values.global.kacho.identity.domain }}/
    cors:
      enabled: true
      allowed_origins:
        - https://{{ .Values.global.kacho.identity.appSubdomain }}.{{ .Values.global.kacho.identity.domain }}
      allowed_methods: [POST, GET, PUT, PATCH, DELETE]
      allowed_headers: [Authorization, Cookie, Content-Type]
      exposed_headers: [Content-Type, Set-Cookie]
  admin:
    base_url: http://kacho-umbrella-kratos-admin:80/

# ─── Identity schema v2 (WebAuthn + email) ──────────────────────────
identity:
  default_schema_id: kacho_user_v2
  schemas:
    - id: kacho_user_v2
      url: file:///etc/kacho-identity/identity.schema.json

# ─── Selfservice flows ──────────────────────────────────────────────
selfservice:
  default_browser_return_url: https://{{ .Values.global.kacho.identity.appSubdomain }}.{{ .Values.global.kacho.identity.domain }}/
  allowed_return_urls:
    - https://{{ .Values.global.kacho.identity.appSubdomain }}.{{ .Values.global.kacho.identity.domain }}/

  methods:
    # WebAuthn / Passkey — primary authentication method (acceptance §5.1).
    webauthn:
      enabled: true
      config:
        passwordless: true
        rp:
          # RP-id = root домен. WebAuthn Level 3 §5.1.3 разрешает RP-id равным
          # eTLD+1 → credentials работают для всех поддоменов (app., kratos., …).
          id: {{ .Values.global.kacho.identity.domain | quote }}
          display_name: "Kacho Cloud"
          # Origin строго совпадает с UI origin (acceptance §6.1.4 — phishing-resistance).
          origins:
            - https://{{ .Values.global.kacho.identity.appSubdomain }}.{{ .Values.global.kacho.identity.domain }}

    # Password fallback (acceptance §5.1, §6.3.1):
    #   - Argon2id m=64MB t=3 p=4 (Kratos default tuned per OWASP 2024).
    #   - HIBP k-anonymity check enabled (api.pwnedpasswords.com /range/{SHA1-prefix}).
    #   - identifier_similarity_check_enabled — отвергает passwords containing email.
    password:
      enabled: true
      config:
        min_password_length: 8
        identifier_similarity_check_enabled: true
        haveibeenpwned_enabled: true
        haveibeenpwned_host: api.pwnedpasswords.com

    # TOTP secondary (acceptance §6.3.2 — для password+TOTP path → AAL2).
    totp:
      enabled: true
      config:
        issuer: "Kacho Cloud"

    # Backup codes (lookup_secret) — для recovery edge-case (acceptance §2.8).
    lookup_secret:
      enabled: true

    # Profile / link / oidc / code — disabled (Phase 2 single-IdP model).
    link:
      enabled: false
    code:
      enabled: true   # required for recovery magic-link mode
      config:
        lifespan: 5m
    profile:
      enabled: true
    oidc:
      enabled: false  # Federation — Phase 11, не Phase 2.

  flows:
    # ─── Registration ────────────────────────────────────────────
    registration:
      enabled: true
      ui_url: https://{{ .Values.global.kacho.identity.appSubdomain }}.{{ .Values.global.kacho.identity.domain }}/auth/registration
      lifespan: 30m
      after:
        webauthn:
          hooks:
            # C4 fix: provisioning web_hook → kacho-iam :9092 HTTP hooks
            # listener (Service kacho-iam-internal). The previous target was
            # the PURE gRPC :9091 port with a REST-style path that does not
            # exist there → every hook failed silently → users were never
            # mirrored into kacho_iam (no project/namespace). The handler
            # (internal/handler/iamhooks/provision_hook_handler.go) calls the
            # UpsertFromIdentity use-case in-process.
            - hook: web_hook
              config:
                url: {{ .Values.global.kacho.identity.hooks.scheme }}://{{ include "kacho.identity.hooksAuthority" . }}/iam/v1/hooks/provision
                method: POST
                body: file:///etc/kacho-identity/hooks/identity-payload.jsonnet
                auth:
                  type: api_key
                  config:
                    in: header
                    name: X-Kacho-Hook-Token
                    value: ${KACHO_IAM_HOOK_TOKEN}
            - hook: show_verification_ui
            - hook: session   # Kratos auto-issues session post-registration → AAL2
        password:
          hooks:
            # C4 fix — see webauthn note above (same :9092 HTTP route).
            - hook: web_hook
              config:
                url: {{ .Values.global.kacho.identity.hooks.scheme }}://{{ include "kacho.identity.hooksAuthority" . }}/iam/v1/hooks/provision
                method: POST
                body: file:///etc/kacho-identity/hooks/identity-payload.jsonnet
                auth:
                  type: api_key
                  config:
                    in: header
                    name: X-Kacho-Hook-Token
                    value: ${KACHO_IAM_HOOK_TOKEN}
            - hook: show_verification_ui

    # ─── Login ───────────────────────────────────────────────────
    login:
      ui_url: https://{{ .Values.global.kacho.identity.appSubdomain }}.{{ .Values.global.kacho.identity.domain }}/auth/login
      lifespan: 30m
      after:
        webauthn:
          hooks:
            # C4 fix — provisioning web_hook → kacho-iam :9092 HTTP route
            # (see the registration.after note above for the rationale).
            - hook: web_hook
              config:
                url: {{ .Values.global.kacho.identity.hooks.scheme }}://{{ include "kacho.identity.hooksAuthority" . }}/iam/v1/hooks/provision
                method: POST
                body: file:///etc/kacho-identity/hooks/identity-payload.jsonnet
                auth:
                  type: api_key
                  config:
                    in: header
                    name: X-Kacho-Hook-Token
                    value: ${KACHO_IAM_HOOK_TOKEN}
            - hook: require_verified_address
        password:
          hooks:
            # C4 fix — provisioning web_hook → kacho-iam :9092 HTTP route.
            - hook: web_hook
              config:
                url: {{ .Values.global.kacho.identity.hooks.scheme }}://{{ include "kacho.identity.hooksAuthority" . }}/iam/v1/hooks/provision
                method: POST
                body: file:///etc/kacho-identity/hooks/identity-payload.jsonnet
                auth:
                  type: api_key
                  config:
                    in: header
                    name: X-Kacho-Hook-Token
                    value: ${KACHO_IAM_HOOK_TOKEN}
            - hook: require_verified_address

    # ─── Settings ────────────────────────────────────────────────
    # Privileged flow — re-authentication every 15min.
    settings:
      ui_url: https://{{ .Values.global.kacho.identity.appSubdomain }}.{{ .Values.global.kacho.identity.domain }}/auth/settings
      lifespan: 30m
      privileged_session_max_age: 15m
      required_aal: highest_available

    # ─── Recovery (magic-link, 5min TTL, IP-bound) ────────────────
    # acceptance §2.8: single-use, IP-bound (Kratos records issuing IP; on
    # redemption verifies X-Forwarded-For === issuing IP — strict exact match
    # в Phase 2; relaxed /24 — Phase 12 hardening per §10 Q5).
    recovery:
      enabled: true
      ui_url: https://{{ .Values.global.kacho.identity.appSubdomain }}.{{ .Values.global.kacho.identity.domain }}/auth/recovery
      lifespan: 5m
      use: code
      after:
        hooks:
          - hook: revoke_active_sessions
          # Завершение восстановления → :9092 HTTP hooks listener, как у трёх
          # соседних хуков. Прежде здесь стоял ЛЕГАСИ gRPC-порт с REST-подобным
          # путём, которого на чистом gRPC не существует: хук падал молча,
          # провайдер считал вызов сделанным, и событие не доезжало НИКОГДА —
          # восстановивший доступ оставался заблокированным, а прежние сессии
          # переживали восстановление.
          #
          # Отсрочка объяснялась тем, что «OnRecoveryCompleted не реализован».
          # Утверждение пережило свой предмет: use-case существовал
          # (internal/apps/kacho/api/user/internal_on_recovery.go) и вызывался
          # по внутреннему gRPC; не хватало ровно HTTP-маршрута к нему. Маршрут
          # заведён (internal/handler/iamhooks/recovery_hook_handler.go), и его
          # НАЛИЧИЕ держит проба, а не память автора.
          - hook: web_hook
            config:
              url: {{ .Values.global.kacho.identity.hooks.scheme }}://{{ include "kacho.identity.hooksAuthority" . }}/iam/v1/hooks/recovery
              method: POST
              body: file:///etc/kacho-identity/hooks/recovery-payload.jsonnet
              auth:
                type: api_key
                config:
                  in: header
                  name: X-Kacho-Hook-Token
                  value: ${KACHO_IAM_HOOK_TOKEN}

    # ─── Verification ────────────────────────────────────────────
    verification:
      enabled: true
      ui_url: https://{{ .Values.global.kacho.identity.appSubdomain }}.{{ .Values.global.kacho.identity.domain }}/auth/verification
      lifespan: 15m
      use: code

    # ─── Logout ──────────────────────────────────────────────────
    logout:
      after:
        default_browser_return_url: https://{{ .Values.global.kacho.identity.appSubdomain }}.{{ .Values.global.kacho.identity.domain }}/

    # ─── Error ───────────────────────────────────────────────────
    error:
      ui_url: https://{{ .Values.global.kacho.identity.appSubdomain }}.{{ .Values.global.kacho.identity.domain }}/auth/error

# ─── Session ────────────────────────────────────────────────────────
session:
  # 24h browser session; Hydra issues 15min access_token / 30d refresh_token
  # (отдельный жизненный цикл — acceptance §2.3).
  lifespan: 24h
  cookie:
    domain: {{ .Values.global.kacho.identity.domain | quote }}
    same_site: Lax
    path: /
    persistent: true
  whoami:
    required_aal: aal1   # whoami доступен с AAL1; AAL2 — required для protected RPC.

# ─── Hashers ────────────────────────────────────────────────────────
hashers:
  algorithm: argon2
  argon2:
    memory: 64MB
    iterations: 3
    parallelism: 4
    salt_length: 16
    key_length: 32

# ─── Courier (SMTP) ─────────────────────────────────────────────────
# Email-channel для magic-link recovery + verification. В dev — MailHog
# catch-all; в prod — SMTP-relay через external-secrets.
courier:
  smtp:
    connection_uri: {{ .Values.global.kacho.identity.smtp.connectionURI | quote }}
    from_address: {{ .Values.global.kacho.identity.smtp.fromAddress | quote }}
    from_name: {{ .Values.global.kacho.identity.smtp.fromName | quote }}

# ─── Secrets ────────────────────────────────────────────────────────
# Cookie + cipher secrets — 32 bytes random; в prod через external-secrets.
secrets:
  cookie:
    - ${KRATOS_SECRETS_COOKIE}
  cipher:
    - ${KRATOS_SECRETS_CIPHER}

# ─── Logging ────────────────────────────────────────────────────────
log:
  level: info
  format: json
  leak_sensitive_values: false
{{- end -}}

{{/* Схема личности (`identity.schema.json`). */}}
{{- define "kacho.identity.schemaJSON" -}}
{
  "$id": "https://schemas.{{ .Values.global.kacho.identity.domain }}/identity/v2",
  "$schema": "http://json-schema.org/draft-07/schema#",
  "title": "Kacho User Identity v2",
  "type": "object",
  "properties": {
    "traits": {
      "type": "object",
      "properties": {
        "email": {
          "type": "string",
          "format": "email",
          "title": "Email",
          "minLength": 3,
          "maxLength": 320,
          "ory.sh/kratos": {
            "credentials": {
              "password": { "identifier": true },
              "webauthn":  { "identifier": true }
            },
            "verification": { "via": "email" },
            "recovery":     { "via": "email" }
          }
        },
        "display_name": {
          "type": "string",
          "title": "Display Name",
          "minLength": 1,
          "maxLength": 128
        }
      },
      "required": ["email"],
      "additionalProperties": false
    }
  }
}
{{- end -}}

{{/* Нагрузка обратного вызова заведения пользователя. */}}
{{- define "kacho.identity.hookIdentityPayload" -}}
function(ctx) {
  // Kratos identity id (UUID) — used as the kacho external_id / OIDC sub.
  external_id: ctx.identity.id,
  // Primary email trait (required by identity.schema.json).
  email: ctx.identity.traits.email,
  // Optional human-readable name; default to empty string when absent so
  // the handler always receives a well-formed object.
  display_name: if std.objectHas(ctx.identity.traits, "display_name")
    then ctx.identity.traits.display_name
    else "",
}
{{- end -}}

{{/* Нагрузка обратного вызова завершения восстановления доступа. */}}
{{- define "kacho.identity.hookRecoveryPayload" -}}
function(ctx) {
  // Kratos identity id (UUID) — тот же external_id, что у заведения
  // пользователя: обе стороны обязаны называть человека одинаково, иначе
  // восстановление найдёт не ту строку либо не найдёт ни одной.
  external_id: ctx.identity.id,
  // Адрес, по которому шло восстановление.
  email: ctx.identity.traits.email,
  // Идентификатор события восстановления — ключ идемпотентности: провайдер
  // вправе повторить доставку, и повтор обязан быть no-op, а не вторым
  // сдвигом отсечки сессий. Берётся id потока, а не время: время у двух
  // доставок одного события разное, и повтор перестал бы опознаваться.
  recovery_jti: ctx.flow.id,
}
{{- end -}}

{{/*
kacho.identity.configRenderInitContainer — подстановка величины обратного вызова
ДО старта процесса личности.

ЗАЧЕМ. Конфигурация объявляет учётные данные обратных вызовов как
`${KACHO_IAM_HOOK_TOKEN}`, то есть рассчитывает на подстановку. Служба личности
подстановки в ЗНАЧЕНИЯХ конфигурации не делает: она переопределяет ключи
переменными по пути ключа, а элемент массива хуков таким путём невыразим.
Строка уезжала в заголовок ДОСЛОВНО, служба прав отвечала 401, провайдер — 502;
вход паролем не проходил, посев церемонии не вставал, девять коллекций
оставались без отчёта (прогон 32532668160, задача #948).

ГДЕ ЖИВЁТ ВЕЛИЧИНА. Только в памяти этих двух контейнеров: в карту настроек она
не попадает, в рендер чарта — тоже. Secret заводится посевом стенда, чарту его
величина неизвестна by construction.

ОТКАЗ ЗАКРЫТЫЙ. Пустая величина и любая неподставленная ссылка роняют запуск с
текстом, называющим ручку. Молчаливый проход означал бы ровно тот дефект,
который здесь чинится: полоса исполняется и отвергается.

ОБРАЗ — ТОТ ЖЕ, что у главного контейнера: ничего нового не тянем, и версия
совпадает by construction.
*/}}
{{- define "kacho.identity.configRenderInitContainer" -}}
- name: identity-config-render
  image: {{ include "kratos.image" . }}
  imagePullPolicy: {{ .Values.image.pullPolicy | default "IfNotPresent" }}
  securityContext:
    allowPrivilegeEscalation: false
    readOnlyRootFilesystem: true
    runAsNonRoot: true
    runAsUser: 10000
    capabilities:
      drop: ["ALL"]
    seccompProfile:
      type: RuntimeDefault
  command: ["sh", "-euc"]
  args:
    - |
      : "${KACHO_IAM_HOOK_TOKEN:?величина обратного вызова пуста: Secret kacho-iam-hook-token/token не доехал до пода — полоса обратных вызовов была бы отвергнута службой прав}"
      awk -v tok="$KACHO_IAM_HOOK_TOKEN" \
          'BEGIN { gsub(/[&\\]/, "\\\\&", tok) }
           { gsub(/\$\{KACHO_IAM_HOOK_TOKEN\}/, tok); print }' \
          /etc/kacho-identity-src/kratos.yaml > /etc/kacho-identity-rendered/kratos.yaml
      if grep -q 'KACHO_IAM_HOOK_TOKEN' /etc/kacho-identity-rendered/kratos.yaml; then
        echo "ОТКАЗ: в конфигурации осталась неподставленная ссылка на величину обратного вызова" >&2
        exit 1
      fi
      echo "подстановка исполнена: $(wc -l < /etc/kacho-identity-rendered/kratos.yaml) строк"
  env:
    - name: KACHO_IAM_HOOK_TOKEN
      valueFrom:
        secretKeyRef:
          name: kacho-iam-hook-token
          key: token
  volumeMounts:
    - name: kacho-identity-config
      mountPath: /etc/kacho-identity-src
      readOnly: true
    - name: kacho-identity-rendered
      mountPath: /etc/kacho-identity-rendered
{{- end -}}
