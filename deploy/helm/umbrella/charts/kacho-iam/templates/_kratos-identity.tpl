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
{{- /* ── БРАУЗЕРНЫЕ АДРЕСА: ВЫВОДЯТСЯ, НО ПЕРЕОПРЕДЕЛИМЫ ПРОФИЛЕМ ─────────────
     Ниже — адреса, по которым ходит БРАУЗЕР, а не под. Выводить их из одного
     `domain` можно ровно там, где консоль стоит на `https://<app>.<domain>`
     без порта. На стенде разработки она стоит на `http://console.kacho.local:28080`
     (kind отображает 80 → 28080, слушателя на 443 нет вовсе), а потоки служба
     раздаёт в КОРНЕ (`^/(login|registration|…)` в nginx консоли), без префикса
     `/auth`. Невыразимость этой посадки означала не «профиль не настроен», а
     `https://app.api.kacho.cloud/auth/registration` в браузере стенда: имя не
     разрешается, и ни одна проба консоли не доходит до продукта.

     Идиома та же, что у `hooks.host`/`hooks.port` выше: ПУСТО ⇒ вывести.
     Поэтому боевой профиль, который ничего из этого не объявляет, рендерится
     байт-в-байт как прежде. */ -}}
{{- $id := .Values.global.kacho.identity -}}
{{- $app := ($id.appBaseURL | default (printf "https://%s.%s" $id.appSubdomain $id.domain)) -}}
{{- $kratosPublic := ($id.kratosPublicBaseURL | default (printf "https://%s.%s/" $id.kratosSubdomain $id.domain)) -}}
{{- $flow := printf "%s%s" $app ($id.flowPathPrefix | toString) -}}
{{- /* ── ПОСАДКА БЕЗ ДОМЕННОГО ИМЕНИ ─────────────────────────────────────────
     `domainless: true` объявляет ФАКТ о посадке: у её внешнего origin
     доменного имени НЕТ — браузер приходит на голый IP-литерал. Следствий
     два, и оба ВЫНУЖДЕНЫ стандартами, а не выбраны нами:

       1. печенье сессии становится host-only — ключ `domain` НЕ печатается
          вовсе. RFC 6265 §5.1.3 (domain matching) начинается с проверки
          «если строка хоста есть IP-адрес, совпадение ЛОЖНО, кроме дословного
          равенства», а §5.3 п.6 отбрасывает печенье, чей `Domain` не
          domain-matches хосту. То есть ЛЮБОЕ значение `domain` на IP-посадке
          заставляет браузер отбросить печенье целиком: вход не состоится, и
          отказ будет молчаливым — сервер выдал печенье, браузер его не принял;
       2. ключи доступа (webauthn) выключаются. RP ID по WebAuthn L3 §5.1.3 и
          §13.4.1 обязан быть ВАЛИДНЫМ ДОМЕННЫМ ИМЕНЕМ, а origin — иметь
          registrable domain suffix; у IP-литерала его нет by construction.
          Выразить `rp.id` нечем, а выдуманный `id` дал бы полосу входа,
          которую браузер отвергает на КАЖДОЙ попытке, — то есть объявленную
          возможность, не работающую ни при каком вводе. Поэтому полоса
          объявляется выключенной ЯВНО: вход на такой посадке идёт паролем
          (+TOTP), и это СКАЗАНО, а не выведено читателем из отсутствия ключа.

          Форма, которую мы при этом печатаем (`webauthn: {enabled: false}`,
          без блока `config`), провайдером принимается — и это ЗАМЕРЕНО, а не
          предположено: ПЕРВЫЙ `--config` этого же пода (значения подчарта
          провайдера, `kratos.kratos.config`) перечисляет методы БЕЗ webauthn
          вовсе, то есть стенд уже живёт с этой полосой в состоянии по
          умолчанию. Отдельно НЕ проверялось (бинаря провайдера в дереве нет),
          требует ли его схема `rp.id` при `enabled: true` — на решение это не
          влияет: довод выше не про схему, а про браузер.

     ПОЧЕМУ ОТДЕЛЬНАЯ РУЧКА, А НЕ «ПУСТО ⇒ НЕ ПЕЧАТАТЬ». Пустое значение у
     `cookieDomain`/`webauthnRpId` уже ЗАНЯТО и означает «не объявлено ⇒
     вывести из `domain`»; на нём стоит боевой профиль, который их не
     объявляет вовсе. Дать пустому второй смысл значило бы молча снять
     `domain` печенья в бою. Отсутствие обязано быть представимо ОТДЕЛЬНО от
     значения — здесь это отдельная ручка, а не перегруженное пустое.

     ПОЧЕМУ ОДНА РУЧКА НА ДВА СЛЕДСТВИЯ. Факт о посадке один: доменного имени
     нет. Две ручки об одном факте разошлись бы молча, и посадка получила бы
     host-only печенье при включённых ключах доступа — состояние, которого не
     выбирал никто.

     ОТКАЗ ЗАКРЫТЫЙ. Объявив посадку без домена, профиль обязан ОЧИСТИТЬ
     `cookieDomain`/`webauthnRpId`, если их объявил слой под ним: иначе
     значение принято и молча выброшено — запрещённый исход. */ -}}
{{- $domainless := $id.domainless | default false -}}
{{- if $domainless -}}
{{- if or $id.cookieDomain $id.webauthnRpId -}}
{{- fail (printf "identity: посадка объявлена без доменного имени (global.kacho.identity.domainless=true), но cookieDomain=%q и webauthnRpId=%q не пусты — на IP-посадке они НЕ ПРИМЕНЯЮТСЯ (RFC 6265 §5.1.3 · WebAuthn L3 §5.1.3), и молчаливо выбросить их нельзя. Очистите обе ручки в профиле этой посадки либо снимите domainless" ($id.cookieDomain | toString) ($id.webauthnRpId | toString)) -}}
{{- end -}}
{{- end -}}
{{- $cookieDomain := ($id.cookieDomain | default $id.domain) -}}
{{- $rpID := ($id.webauthnRpId | default $id.domain) -}}
{{- /* ── СХЕМЫ, УНАСЛЕДОВАННЫЕ ОТ СЛОЯ ПОД НАМИ ──────────────────────────────
     Процесс получает ДВА файла настроек и сливает их по порядку; наш идёт
     ВТОРЫМ. Слияние идёт по ключам, а `identity.schemas` — СПИСОК: он не
     дополняется, он ЗАМЕЩАЕТСЯ целиком. Значит всякая схема, объявленная слоем
     под нами, из конфигурации ИСЧЕЗАЕТ — молча, без диагностики при старте.

     Цена исчезновения ложится не на новые регистрации (им умолчанием остаётся
     `kacho_user_v2`), а на УЖЕ СУЩЕСТВУЮЩИЕ строки: у личности имя схемы
     записано в её собственной строке, и чтение личности, чья схема не
     объявлена, отвечает `500 invalid_configuration`. Арендатор, заведённый до
     смены схемы, перестаёт читаться вовсе, а выглядит это отказом сервиса.

     ПЕРЕВЕСТИ СТРОКИ НА НОВУЮ СХЕМУ НЕЛЬЗЯ, и это не вопрос усилий: обе схемы
     строгие (`additionalProperties: false`), у унаследованной есть признак,
     которого у `kacho_user_v2` нет, — валидация по v2 отвергла бы каждую такую
     строку. Значит унаследованная схема обязана оставаться ОБЪЯВЛЕННОЙ ровно
     столько, сколько живут ссылающиеся на неё строки.

     ТЕЛО СХЕМЫ ЗДЕСЬ НЕ ЖИВЁТ, И ЭТО НАМЕРЕННО. Оно приезжает значением
     профиля — тем же узлом YAML, которым объявлено у слоя под нами (якорь и
     ссылка в одном файле). Копия тела здесь была бы вторым местом об одном
     предмете, и разошлись бы они в одну сторону: процесс берёт то объявление,
     что стоит позже, то есть НАШЕ. При якоре расхождение невозможно by
     construction, а не доказывается проверкой.

     УМОЛЧАНИЕ — ПУСТО, и цена названа: посадка, никогда не объявлявшая чужой
     схемы, лишнего объявления не получает; посадка, которая её объявляла,
     получает покрытие БЕЗ ручной настройки — ссылка стоит в том же файле, что
     и объявление, и приезжает вместе с ним в каждый профиль, который этот файл
     наследует. Не покрыто ровно одно: схема, объявленная в ПРОШЛОМ и уже
     снятая из дерева, — ссылающиеся на неё строки живы, а объявления нет
     нигде. Это видно только на кластере; здесь оно названо, а не закрыто.

     Держит deploy/identity_inherited_schemas_are_declared_test.go. */ -}}
{{- $inherited := $id.inheritedSchemas | default list -}}
{{- range $inherited -}}
{{- if eq (.id | toString) "kacho_user_v2" -}}
{{- fail (printf "identity: унаследованная схема названа %q — тем же именем, что и собственная схема продукта. Двух схем под одним именем в одной конфигурации не бывает: позднейшее объявление молча вытеснит раннее, и какое из двух тел применится к живым строкам, будет решать порядок, а не решение. Переименуйте унаследованную схему либо снимите её из global.kacho.identity.inheritedSchemas" (.id | toString)) -}}
{{- end -}}
{{- end -}}
version: v1.3.1

# ─── Serve endpoints ────────────────────────────────────────────────
serve:
  public:
    base_url: {{ $kratosPublic }}
    cors:
      enabled: true
      allowed_origins:
        - {{ $app }}
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
{{- range $inherited }}
    # Унаследовано от слоя под нами: строки личностей, созданные до смены
    # схемы, ссылаются на неё по ИМЕНИ и без объявления не читаются.
    - id: {{ .id | toString | quote }}
      url: {{ .url | toString | quote }}
{{- end }}

# ─── Selfservice flows ──────────────────────────────────────────────
selfservice:
  default_browser_return_url: {{ $app }}/
  allowed_return_urls:
    - {{ $app }}/

  methods:
    # ─── КАКОЙ МЕТОД ЗДЕСЬ ВТОРОЙ ФАКТОР (#1213) ─────────────────────────
    #
    # Каталог прав объявляет 32 глаголам пол уровня уверенности «2», и край
    # спрашивает его в том числе на браузерной полосе. Значит ХОТЯ БЫ ОДИН
    # метод ниже обязан быть ВТОРЫМ фактором, иначе объявленный пол означает
    # не «подтвердите второй фактор», а «этого действия из браузера не
    # существует» — для всех, а не для нарушителей.
    #
    # Вторым фактором объявлены `totp` (одноразовый код по времени) и
    # `lookup_secret` (запасные коды). Выбор одноразового кода основным
    # обоснован тем, что он не требует оборудования и заводится арендатором
    # самостоятельно, — то есть уровень поднимается ИЗ КОНСОЛИ, без ключа
    # доступа, полученного заранее.
    #
    # ЭТОТ ПЕРЕЧЕНЬ СВЕРЯЕТСЯ С КОНСОЛЬЮ, а не живёт сам по себе: окно
    # повторного подтверждения объявляет способы, которые умеет вести
    # (`ui-future/shared/src/lib/step-up-methods.ts`), и согласие двух сторон
    # держит гейт `deploy/identity_second_factor_reachable_test.go`. Он
    # печатает ОБЕ величины — записей с полом «2» и достижимых из браузера, —
    # потому что одно число скрывает ровно тот случай, ради которого гейт
    # заведён.
    #
    # Правя `enabled` любого метода ниже, помни: выключенный метод и метод, о
    # котором консоль не знает, недостижимы ОДИНАКОВО.

    # WebAuthn / Passkey — ПЕРВЫЙ фактор, а не второй (беспарольная посадка).
    #
    # `passwordless: true` означает, что удостоверение ключа доступа заводится
    # и предъявляется ВМЕСТО пароля: служба личности выдаёт по нему `aal1` и в
    # потоке `aal=aal2` не предлагает его вовсе. Держать его вторым фактором
    # нельзя — это была бы возможность, объявленная и неисполнимая.
    #
    # Перевести ключ доступа во второй фактор (`passwordless: false`) значит
    # снять беспарольный вход, объявленный основным способом: уже заведённые
    # удостоверения перестали бы годиться и для входа, и для подтверждения, а
    # полоса регистрации ключом доступа сломалась бы целиком. Каноничный путь —
    # развести `passkey` (беспарольный) и `webauthn` (второй фактор) отдельными
    # методами, и это переделка способа ВХОДА, а не достижимости уровня: она
    # идёт своим предметом (#1188, вход, устойчивый к посреднику).
    webauthn:
{{- if $domainless }}
      # Посадка без доменного имени: RP ID выразить нечем (WebAuthn L3 §5.1.3
      # требует валидное доменное имя, у IP-литерала его нет). Полоса объявлена
      # выключенной ЯВНО — см. разбор у `$domainless` в шапке этого шаблона.
      enabled: false
{{- else }}
      enabled: true
      config:
        passwordless: true
        rp:
          # RP-id = root домен. WebAuthn Level 3 §5.1.3 разрешает RP-id равным
          # eTLD+1 → credentials работают для всех поддоменов (app., kratos., …).
          id: {{ $rpID | quote }}
          display_name: "Kacho Cloud"
          # Origin строго совпадает с UI origin (acceptance §6.1.4 — phishing-resistance).
          origins:
            - {{ $app }}
{{- end }}

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

    # TOTP — ОСНОВНОЙ второй фактор продукта (acceptance §6.3.2; #1213).
    #
    # Поднимает сессию до `aal2` и не требует ни оборудования, ни выданного
    # заранее ключа: арендатор заводит его сам, в разделе параметров
    # безопасности по адресу потока `settings` ниже. Раздаётся этот адрес с
    # ТОГО ЖЕ имени, что и консоль (её раздача проксирует пути потоков службе
    # личности), поэтому для арендатора это одно место, а не два. Поток параметров
    # объявлен `required_aal: highest_available` ниже — значит арендатор,
    # вошедший ОДНИМ ПАРОЛЕМ, до него доходит и заводит первый фактор второго
    # уровня сам. Курицы и яйца здесь нет, и это свойство обязано остаться при
    # любой правке `settings.required_aal`.
    totp:
      enabled: true
      config:
        issuer: "Kacho Cloud"

    # Backup codes (lookup_secret) — второй фактор ПОСЛЕДНЕГО РУБЕЖА
    # (acceptance §2.8): годится, когда приложение-аутентификатор недоступно.
    # Тоже даёт `aal2`, поэтому объявлен вторым фактором наравне с кодом по
    # времени, но повседневным способом не считается — окно повторного
    # подтверждения предлагает его последним.
    lookup_secret:
      enabled: true

    # ─── ОСТАЛЬНЫЕ ПОЛОСЫ: перечень ОТМЕЧЕН И СВЕРЯЕТСЯ (#1256) ──────────
    #
    # Здесь стояло одной строкой «Profile / link / oidc / code — disabled», и
    # это лгало по двум пунктам из четырёх: `code` и `profile` объявлены
    # включёнными тут же, ниже. Хуже прочего первый из них: `code` — метод
    # ШТАТНОГО ВОССТАНОВЛЕНИЯ ДОСТУПА (`flows.recovery.use: code` ниже), то есть
    # перечень называл несуществующим то, на чём держится возврат арендатору
    # потерянного доступа.
    #
    # Цена почти была уплачена: приёмка соседней фазы обосновывала решение этим
    # перечнем, и неверность нашлась только потому, что перепись блока прогнали
    # машинно, а не поверили комментарию.
    #
    # Поэтому перечень теперь отмечен и сверяется с объявлением:
    #
    #   ВЫКЛЮЧЕНЫ: link, oidc
    #   ВКЛЮЧЕНЫ: code, profile
    #
    # Держит это `deploy/identity_method_comment_matches_declaration_test.go`.
    # Он судит по РАЗОБРАННОМУ объявлению ниже, а не по тексту выше: сверять
    # комментарий с комментарием значило бы проверять согласие лжи с самой
    # собой. И читает он только отмеченные строки — прозу этого разбора, где
    # имена полос стоят рядом со словами о состоянии, он не читает вовсе, иначе
    # краснел бы на собственном объяснении.
    #
    # Правя `enabled` любой полосы ниже, правь и отмеченные строки: перечень
    # отключённых обязан быть ПОЛНЫМ, а не выборочным, — это тоже держит гейт.
    link:
      enabled: false  # Магической ссылки нет: восстановление идёт кодом (см. ниже).
    code:
      enabled: true   # Штатное восстановление доступа — `flows.recovery.use: code`.
      config:
        lifespan: 5m
    profile:
      enabled: true   # Правка своих данных из консоли (поток `settings`).
    oidc:
      enabled: false  # Federation — Phase 11, не Phase 2.

  flows:
    # ─── Registration ────────────────────────────────────────────
    registration:
      enabled: true
      ui_url: {{ $flow }}/registration
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
            # Сессия выдаётся и на этой полосе — как на соседней. Показ экрана
            # подтверждения почты выдачу НЕ заменяет: это разные вещи, и без
            # этого хука арендатор заводился, видел экран подтверждения и
            # оставался СНАРУЖИ, тогда как соседняя полоса того же потока
            # сессию выдавала. Полосы одного потока обязаны сходиться в том,
            # чем поток заканчивается; держит это
            # deploy/identity_registration_lanes_issue_a_session_test.go.
            - hook: session

    # ─── Login ───────────────────────────────────────────────────
    login:
      ui_url: {{ $flow }}/login
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
      ui_url: {{ $flow }}/settings
      lifespan: 30m
      privileged_session_max_age: 15m
      required_aal: highest_available

    # ─── Recovery (magic-link, 5min TTL, IP-bound) ────────────────
    # acceptance §2.8: single-use, IP-bound (Kratos records issuing IP; on
    # redemption verifies X-Forwarded-For === issuing IP — strict exact match
    # в Phase 2; relaxed /24 — Phase 12 hardening per §10 Q5).
    recovery:
      enabled: true
      ui_url: {{ $flow }}/recovery
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
      ui_url: {{ $flow }}/verification
      lifespan: 15m
      use: code

    # ─── Logout ──────────────────────────────────────────────────
    logout:
      after:
        default_browser_return_url: {{ $app }}/

    # ─── Error ───────────────────────────────────────────────────
    error:
      ui_url: {{ $flow }}/error

# ─── Session ────────────────────────────────────────────────────────
session:
  # 24h browser session; Hydra issues 15min access_token / 30d refresh_token
  # (отдельный жизненный цикл — acceptance §2.3).
  lifespan: 24h
  cookie:
{{- /* Ключа НЕТ на посадке без доменного имени: печенье становится host-only.
       Любое значение здесь на IP-посадке заставляет браузер отбросить печенье
       ЦЕЛИКОМ (RFC 6265 §5.1.3) — вход не состоится, и молча. Комментарий
       ШАБЛОННЫЙ, а не отрендеренный: содержимое настроек участвует в отпечатке,
       который перекатывает под, поэтому пояснение внутри него меняло бы
       отпечаток посадкам, которых эта правка не касается. */ -}}
{{- if not $domainless }}
    domain: {{ $cookieDomain | quote }}
{{- end }}
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

ОСТАТОК СУДИТСЯ ПО ФОРМЕ ССЫЛКИ, А НЕ ПО ОДНОМУ ИМЕНИ (задача #1677). Здесь
стояла проверка по конкретному имени, а в той же карте настроек уже жили ссылки
ТОЙ ЖЕ ФОРМЫ вне её поля зрения. Проверка по перечню имён растёт вместе с
перечнем и НЕ растёт вместе с деревом: следующая величина уехала бы
неподставленной, а шаг смолчал бы — потому что искал не её. Форма, о которой
проверка не знает, не даёт ни красного, ни зелёного; она МОЛЧИТ.

ЧЕМ ШАГ ВЛАДЕЕТ — ОБЪЯВЛЕНО ПЕРЕМЕННОЙ РЯДОМ, а не выведено из окружения, и это
не избыточность. Ссылка на секрет НЕОБЯЗАТЕЛЬНА (ниже сказано, почему), поэтому
отсутствующий секрет даёт ОТСУТСТВУЮЩУЮ переменную — неотличимую от ссылки, чей
источник переменная по ПУТИ КЛЮЧА самой службы личности (`secrets.cookie` →
`SECRETS_COOKIE`). Без объявления отказ перестал бы быть закрытым ровно в том
случае, ради которого он заведён. Согласие объявления с перечнем переменных
этого же контейнера держит deploy/identity_substitution_judges_the_form_test.go,
а не внимание: имя, попавшее в одно и не попавшее в другое, роняет прогон.

ССЫЛКА, У КОТОРОЙ ПЕРЕМЕННОЙ НЕТ, ЗДЕСЬ НЕ ОТВЕРГАЕТСЯ — и это решение, а не
послабление: её источник законен и другой (переменная по пути ключа). Что он
ЕСТЬ, судит deploy/tests/helm/identity-hook-credential-source-test.sh по
отрендеренным подам, где путь ключа вычислим. Шаг же называет такие ссылки в
своей переписи, поэтому «ноль находок» здесь отличимо от «ноль прочитанного».

ВЕЛИЧИНА ПОДСТАВЛЯЕТСЯ ДОСЛОВНО. Замена идёт разбором строки, а не заменой по
образцу: в замене по образцу `&` означает совпадение, а `\` — экранирование,
поэтому величина, их содержащая, доехала бы искажённой. Прежняя редакция вдобавок
передавала величину доводом `-v`, который сам обрабатывает escape-последовательности.

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
      src=/etc/kacho-identity-src/kratos.yaml
      out=/etc/kacho-identity-rendered/kratos.yaml

      # Ссылки находятся ПО ФОРМЕ. Ни одно имя здесь не выписано: перечень имён
      # рос бы вместе с конфигурацией и не рос бы вместе с деревом.
      refs=$(grep -oE '\$\{[A-Za-z_][A-Za-z0-9_]*\}' "$src" | tr -d '${}' | sort -u || true)
      if [ -z "$refs" ]; then
        echo "ОТКАЗ: в конфигурации нет НИ ОДНОЙ ссылки формы \${ИМЯ} — подставлять нечего. Это отказ, а не успех: либо конфигурация переписана, либо разбор ослеп" >&2
        exit 1
      fi

      # Чем шаг владеет — объявлено переменной ниже; почему не выведено из
      # окружения, сказано в шапке этого шаблона.
      : "${KACHO_IDENTITY_SUBSTITUTED_VARS:?перечень имён, которыми владеет шаг подстановки, не объявлен — судить остаток не с чем, и молчаливый проход вернул бы дефект #948}"
      for n in $KACHO_IDENTITY_SUBSTITUTED_VARS; do
        eval "v=\${$n-}"
        if [ -z "$v" ]; then
          echo "ОТКАЗ: величина \${$n} пуста или не доехала до пода — Secret отсутствует либо пуст; полоса, которая её несёт, была бы отвергнута принимающей стороной, а стенд при этом выглядел бы поднятым" >&2
          exit 1
        fi
      done

      owned=""
      for n in $refs; do
        eval "v=\${$n-}"
        if [ -n "$v" ]; then owned="$owned $n"; fi
      done

      cp "$src" "$out"
      for n in $owned; do
        eval "KACHO_SUBST_TOKEN=\$$n"
        export KACHO_SUBST_TOKEN
        awk -v name="$n" \
            'BEGIN { pat = "${" name "}"; tok = ENVIRON["KACHO_SUBST_TOKEN"] }
             { line = $0; out = ""
               while ((i = index(line, pat)) > 0) {
                 out = out substr(line, 1, i - 1) tok
                 line = substr(line, i + length(pat))
               }
               print out line }' "$out" > "$out.tmp"
        mv "$out.tmp" "$out"
      done
      unset KACHO_SUBST_TOKEN

      left=$(grep -oE '\$\{[A-Za-z_][A-Za-z0-9_]*\}' "$out" | tr -d '${}' | sort -u || true)
      bad=""
      for n in $left; do
        for o in $owned $KACHO_IDENTITY_SUBSTITUTED_VARS; do
          if [ "$n" = "$o" ]; then bad="$bad $n"; fi
        done
      done
      if [ -n "$bad" ]; then
        echo "ОТКАЗ: в конфигурации остались НЕПОДСТАВЛЕННЫМИ ссылки, которыми этот шаг владеет:$bad" >&2
        exit 1
      fi
      echo "подстановка: ссылок формы $(echo $refs | wc -w) · во владении $(echo $owned | wc -w) · осталось $(echo $left | wc -w) (источник по пути ключа:${left:+ $(echo $left)}) · строк $(wc -l < "$out")"
  env:
    # Перечень имён, которыми ВЛАДЕЕТ шаг. Он объявлен рядом с самими
    # переменными, и их согласие держит гейт, а не внимание: имя, попавшее в
    # одно и не попавшее в другое, роняет прогон.
    - name: KACHO_IDENTITY_SUBSTITUTED_VARS
      value: KACHO_IAM_HOOK_TOKEN
    - name: KACHO_IAM_HOOK_TOKEN
      valueFrom:
        secretKeyRef:
          name: kacho-iam-hook-token
          key: token
          # ССЫЛКА НЕОБЯЗАТЕЛЬНА, А ОТКАЗ ВСЁ РАВНО ЗАКРЫТЫЙ — и это не
          # послабление, а перенос отказа туда, где он что-то говорит.
          # Обязательная форма ссылки роняет под ДО старта и молча: сообщение
          # получает планировщик, а не оператор, и в профиле, где посев
          # секретов не позван, под личности просто не поднимется без
          # объяснения. Проверка предусловий это ловит и называет ложным
          # срабатыванием — справедливо: сосед (служба прав) ссылается на тот
          # же секрет необязательной формой.
          # Пустую величину отвергает сам контейнер подстановки — с текстом,
          # называющим ручку. То есть отказ остался закрытым, но стал
          # ЧИТАЕМЫМ.
          optional: true
  volumeMounts:
    - name: kacho-identity-config
      mountPath: /etc/kacho-identity-src
      readOnly: true
    - name: kacho-identity-rendered
      mountPath: /etc/kacho-identity-rendered
{{- end -}}
