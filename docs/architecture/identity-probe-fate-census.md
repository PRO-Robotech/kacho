# Поимённая судьба проб полосы личности в `deploy/`

Задача kacho#2731, волна 4 (#2798) эпика #2564 — снятие внешнего поставщика
личности. Прежде судьба проверок о снимаемом предмете стояла за **группами**:
запись называла признак группы, а файлы за ним поимённо не называл никто.
Здесь у каждого файла `deploy/identity_*_test.go` — ровно одна строка, ровно
один исход из закрытого словаря и довод с координатой.

Ведомость держит гейт `internal/repohygiene/identityprobefate.go`
(`TestIdentityProbeFateLedgerNamesEveryProbe`): файл без строки, строка без
файла, групповая запись, исход вне словаря, довод без координаты в самой пробе,
координата за концом файла, координата, сошедшая со своего предмета (её якорь
не на её строке), и разбивка, разошедшаяся со строками, — красное.
Снята проба — тем же изменением снимаются её строка и якоря её координат.
Когда уйдёт последняя, гейт и этот документ снимаются вместе.

Записана первой задачей #2731; вторым снимающим изменением правлена задачей
kacho#2818 (раздел «Снятие kacho#2818» ниже), третьим — задачей kacho#1276
(раздел «Снятие kacho#1276»).

## Предмет снятия

Снимается **внешний поставщик личности**: подчарты-архивы
`deploy/helm/umbrella/charts/kratos-0.62.1.tgz`, архив его издателя токенов,
каталог `charts/identity-selfservice-ui`, их базы и всё, что существует только ради
их настройки. Архивы, каталог, объявления зависимостей и их базы, выключатели и
настройки поставщика в профилях зонта сняты задачей kacho#1276. Четыре файла НАШЕГО подчарта, которые ничего, кроме их
конфигурации, не производили, сняты задачей kacho#2818 — вместе с ключом посадки
службы и полосой хуков, которых пиненная служба больше не читает:

```
deploy/helm/umbrella/charts/kaname/templates/_identity-provider.tpl
deploy/helm/umbrella/charts/kaname/templates/identity-provider-config-configmap.yaml
deploy/helm/umbrella/charts/kaname/templates/identity-provider-hooks-configmap.yaml
deploy/helm/umbrella/charts/kaname/templates/identity-provider-schema-configmap.yaml
```

Остаётся **посадка `own`** — единственная у службы (kaname#363): ключа посадки у
подчарта больше нет, подчартов поставщика в зонте нет (kacho#1276). Настройки нашей службы рендерит
`charts/kaname/templates/configmap.yaml`; слушателя хуков поставщика у службы нет
вовсе (`deploy/kaname_hooks_port_follows_posture_test.go:18-20`).

## Закрытый словарь исходов

| исход | когда |
|---|---|
| **снять** | проба существует только ради снимаемого предмета: её вход — артефакт поставщика, и у свойства нет носителя в этом дереве после снятия |
| **оставить** | проба судит свою полосу; снимаемое в ней упомянуто (проза, синтетика, текст находки), но предметом не является |
| **переписать** | проба судит живой предмет через снимаемое; после снятия обязана продолжить судить его на НАШЕМ носителе, и носитель назван координатой |

Четвёртого исхода и корзины «прочее» нет. Исход выводится из **разбора
содержимого** — что проба читает и что утверждает, — а не из совпадения слова:
слово с именем поставщика в этом дереве стоит и в наших собственных координатах
(тело `_identity-provider.tpl`, путь `/etc/kaname-identity-rendered/`), а четыре
шаблона поставщика в нашем подчарте после #2759 не несут его в имени, — признак
по слову ошибается в обе стороны.

## Объём осмотренного

- ревизия прочтения проб: `origin/2798` @ `b7608fe9750`; пути четырёх шаблонов
  подчарта и каталога `charts/identity-selfservice-ui` — имена #2759;
- ревизия координат: `2926` @ `f7696c21291` (сборка 1 волны 4, #2926). Координат
  в документе 168 — в строках ведомости 157, вне их 11 (проза и таблица
  «Снимаемое, на котором держится живое»), различных 163. С ревизии прочтения
  17 записей (14 различных координат) сошли со своего предмета — 16 сдвигом
  начала, 1 ростом предмета внутри диапазона (наш блок входа, kacho#2901) — и
  переведены на строки, где предмет стоит сейчас; ещё у 6 строка предмета
  переписана переименованием #2759 и стоит на прежнем номере. Каждая координата
  прибита якорем (раздел «Якоря координат»), и гейт сверяет его со строкой;
  якорь первой строки не единственный в файле у 8;
- файлов в предмете на записи #2731: **51**; после снятия kacho#2818 — **24**,
  строк **9350**; после снятия kacho#1276 — **15**
  (`git ls-files 'deploy/identity_*_test.go' | wc -l`), строк **6073**; по каждому прочитаны шапка-объявление предмета, источники входа
  (что читается с диска и из какого узла значений) и условие вердикта;
- чтобы установить носитель после снятия, прочитаны: `charts/kaname/templates/configmap.yaml`,
  `charts/kaname/values.yaml`, `values.yaml` зонта, `values.own.yaml`, `deploy/stacks.txt`,
  четыре шаблона поставщика в нашем подчарте;
- перекрёстные ссылки на помощники проб — разбором синтаксиса пакета `deploy_test`
  (имя, объявленное в файле пробы и прочитанное другим файлом; локально
  перекрытые имена отсеяны).

## Ведомость

Колонка «предпосылка» отвечает на один вопрос: чем проба питается — **нашей
полосой** (артефакт, который мы производим и после снятия) или **чужой службой**
(артефакт поставщика, включая наши шаблоны, существующие ради него). Координата
`:<строка>` — в самой пробе строки; иначе путь от корня репозитория.

| # | файл | что утверждает | предпосылка | исход | основание (координата) |
|---:|---|---|---|---|---|
| 1 | `identity_bearer_window_ceiling_injection_test.go` | MAIL-51 падает на поднятом сроке, на неотнесённом и на неразбираемом объявлении и молчит на законном близнеце | наша полоса | оставить | вход — рендер нашего блока `authn.login` боевого стека `:18-25`; близнец `:28`, инъекции `:36-46`, `:50`. Прежний вход — копия шаблона поставщика — снят вместе с шаблоном (kacho#2818) |
| 2 | `identity_bearer_window_ceiling_test.go` | всякий срок полосы входа, уходящей письмом, отнесён к ограничивающим окно либо к неограничивающим с причиной, и ограничивающий не выше потолка | наша полоса | оставить | перечень `:62-72` судит рендер нашего блока на каждом стеке `:126-157`; сроки кодов объявляет наш шаблон `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:362`, `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:374` |
| 6 | `identity_chart_premise_reachability_test.go` | тег `helmcharts` зовётся конвейером, и у его раскола остался предмет | наша полоса | оставить | суд над конвейером и `Chart.yaml` `:66-97`, `:117-186`; под тегом остаётся наша проба `deploy/edge_retired_knobs_render_test.go:4`. Поставщик назван только текстом находки `:89-92` |
| 16 | `identity_domainless_landing_injection_test.go` | гейт выразимости посадки без имени падает на печенье с доменом и на происхождении ключей на IP-литерале и молчит на близнеце | наша полоса | оставить | вход — рендер нашей службы цепочкой a8f60d `:31-39`; вердикт — адъюдикатор гейта строки 17 `:41-64` |
| 17 | `identity_domainless_landing_is_expressible_test.go` | посадка без доменного имени выразима нашей полосой: печенье host-only и ни одного происхождения ключей доступа на IP-литерале | наша полоса | оставить | адъюдикатор `:116-138`; печенье нашей сессии объявляет наш шаблон `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:315`. Помощник рендера `:60` зовут живые пробы (`deploy/client_token_knobs_of_the_pin_test.go:229`) |
| 22 | `identity_global_defaults_agree_test.go` | копия `global.kacho.identity` в подчарте совпадает с зонтом | наша полоса | оставить | согласие двух объявлений `:59-116` судит наш узел, который читает наш отправитель `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:612`; проверка адреса хуков снята вместе с предметом (kacho#2818) |
| 23 | `identity_hook_provenance_reaches_every_stand_test.go` | работа, поднимающая стенд, собирает отчёт о происхождении величины обратного вызова | чужая служба | снять | отчёт `:57` снимает отпечатки у отправителя-поставщика и проверяющей стороны `deploy/scripts/identity-hook-credential-provenance.sh:5-8`; отправителя не станет |
| 26 | `identity_mail_defaults_are_empty_injection_test.go` | гейт MAIL-12 падает и молчит на синтетическом слое | наша полоса | оставить | синтетика строится вокруг нашего узла `:26-35` и зовёт вердикт гейта строки 27 `:51-56`, `:81` |
| 27 | `identity_mail_defaults_are_empty_test.go` | у пяти почтовых величин `global.kacho.identity.smtp` нет встроенного умолчания ни в одном слое | наша полоса | оставить | перечень ручек нашего узла `:66-72`, обход слоёв умолчаний `:104-107`, `:188-215`; узел читает наш отправитель `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:612`. Поставщик назван только в границе `:44-45` |
| 28 | `identity_mail_lane_feeds_both_senders_injection_test.go` | MAIL-48 падает на отправителе, питаемом чужим узлом целиком и частично, и на снятом разделе, молчит на законной копии | наша полоса | оставить | копия НАШЕГО шаблона `:21-38`; инъекции `:71-94`, `:100-111` |
| 29 | `identity_mail_lane_feeds_both_senders_test.go` | наш отправитель письма питается ровно узлом `global.kacho.identity.smtp` | наша полоса | оставить | узел решения Р23 `:55`, вердикт `:262-288`; раздел отправителя — наш шаблон `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:623`. Второго отправителя больше нет: настройки поставщика подчарт не производит (kacho#2818) |
| 30 | `identity_mail_lane_single_declaration_injection_test.go` | MAIL-54 падает только на своём предмете по трём осям | наша полоса | оставить | копия дерева с НАШИМ шаблоном `:65`; три оси с близнецами `:111-219` |
| 31 | `identity_mail_lane_single_declaration_test.go` | почтовая полоса объявлена одним местом — разделом нашего отправителя, — и оснастка раскатки посылает оператора туда | наша полоса | оставить | координата питания `:60`; разделы наших настроек выводятся из шаблона `:120-148`; единственное объявление — `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:623` |
| 39 | `identity_second_factor_reachable_injection_test.go` | гейт достижимости второго фактора падает и молчит по каждой стороне | наша полоса | оставить | стороны `own` — объявление консоли и корень пиненной службы `:215`, `:276`; посадка `external` — отказ `:354`. Поставщик назван синтетикой `:51-53`, `:115-127`; самопроверка предиката монтирования ушла вместе с ним (строка 40) |
| 40 | `identity_second_factor_reachable_test.go` | пол уровня уверенности «2» достижим: служба, консоль и каталог прав сходятся | наша полоса | оставить | стороны посадки `own` `:26-41`, `:2282-2296`, `:2332`; путь настроек поставщика и предикат монтирования ушли вместе с пробами строк 14 и 35 (kacho#2818), половина службы — одна посадка, `own` (`kanameLanding`) — предмет файла от этого не меняется |

## Разбивка по исходам

| исход | файлов |
|---|---:|
| снять | 1 |
| оставить | 14 |
| переписать | 0 |
| **итого** | **15** |

Сумма равна числу файлов; файлов без исхода нет, исходов вне словаря нет. Сверка
имён ведомости с деревом, исходов, координат и их якорей — прогоном гейта:

```sh
go test ./internal/repohygiene/ -run 'TestIdentityProbeFate' -count=1 -v
# перепись: строк ведомости 15 · файлов в дереве 15 · снять 1 · оставить 14 · переписать 0 · …
```

## Снимаемое, на котором держится живое

Проба с исходом «снять» объявляет помощники, которые читают ЖИВЫЕ пробы вне этой
ведомости. Снять файл целиком нельзя: помощник обязан переехать к читателю тем
же изменением, иначе пакет не соберётся.

| снимаемая проба | помощник | живой читатель |
|---|---|---|
| `identity_hook_provenance_reaches_every_stand_test.go` | `provWorkflow`, `provStandRecipes` | `deploy/stand_verdict_carries_provenance_test.go:130`, `deploy/stand_verdict_carries_provenance_test.go:147` |

Снятые задачей kacho#1276 пробы унесли своих помощников к читателям тем же
изменением: `declaresProduction` — вместе со своим контролем
`TestDeclaresProduction_RecognisesTheRealTree` — в
`deploy/db_footprint_declaration_test.go`, `itoa` — в
`deploy/trust_anchor_claim_matches_declaration_test.go`. Снятые задачей
kacho#2818 пробы унесли своих помощников к читателям тем же изменением: `scalarLine` — в `deploy/rollout_preflight_covers_required_secrets_test.go`,
`contains` — в `deploy/kaname_listener_knobs_test.go`, `mergedValuesOfStack` — в
`deploy/own_minting_census_test.go`, `renderStack`, `decodeRender`, `str`, `submap`,
`slice` — в `deploy/edge_retired_knobs_render_test.go`. Помощники оставляемых проб,
которые читают живые пробы (`renderIdentitySubchart`, `iamSubchartDir`,
`leafString`, `identityLanding*`), остаются на месте вместе со своими файлами.

## Что изменилось против прежней записи

Прежняя поимённая запись (ветка `issue-2731`, основание `f445aaaa554`) стояла на
другом словаре («перевести», «переутвердить») и на другом дереве. С того
основания:

- состав: проба `identity_seed_matches_chart_schema_test.go` снята вместе с
  посевом (kacho#2858), появилась `identity_config_template_test.go` — дом
  общей координаты, которую прежде держал снятый посев;
- посадку `own` объявили все стенды, стек поставщика выключен в базе зонта, и
  слушатель хуков под `own` не строится. Отсюда другой исход у проб, чей
  носитель после снятия в этом дереве не существует: транспорт обратных
  вызовов (строка 4), отпечаток карты (7), адреса потоков (20, 21),
  перечень полос входа (32, 33), полосы регистрации (34), потоки доставки (13,
  14), требование подтверждённого адреса (48–51) — **снять**, а не переводить:
  переводить не на что;
- проба второго фактора (39, 40) уже судит посадку `own` у пина службы —
  **оставить**; проба раскола тега (6) сохранила популяцию вне поставщика —
  **оставить**; проба умолчаний почты (26, 27) читает только наш узел —
  **оставить**.


## Снятие kacho#2818

Подчарт службы доступа потерял провязку поставщика тем же изменением, которым
поднят пин службы до ревизии с kaname#363 (ключ посадки отвергается при любом
значении), kaname#361 (зеркала набора ключей нет) и kaname#362 (административной
дороги нет). Отсюда:

- **сняты** пробы, чей вход — снятые шаблоны либо ключ посадки: строки 3, 4, 7–10,
  12–14, 18, 19, 24, 25, 32–36, 43–51. Их свойства там, где у них остался предмет,
  держат преемники: отсутствие следа поставщика в рендере каждой цепочки —
  `deploy/stack_render_carries_no_vendor_residue_test.go` (половина own прежней
  строки 19); провязка поставщика в подчарте службы, снятые ключи пина и полоса
  хуков — `deploy/kaname_subchart_retired_identity_wiring_test.go`;
- **переписаны** на НАШ носитель и оставлены — строки 1, 2, 16, 17, 22, 28–31: окно
  кодов письма судит блок `authn.login`, выразимость посадки без имени — печенье и
  происхождения ключей нашей полосы, питание и единственность почтовой полосы —
  раздел `invite-mail` нашего отправителя;
- у проб второго фактора (39, 40) половина службы больше не читается — посадка у
  неё одна; позиция аргумента способов входа у наблюдателя корня выводится из его
  объявления у пина, а не стоит постоянной.

## Снятие kacho#1276

Подчарты поставщика сняты с зонта: объявления зависимостей в `Chart.yaml` и
выключатели в базе зонта — одним изменением, вместе с архивами, каталогом
`charts/identity-selfservice-ui`, базами поставщика, его настройками в профилях,
профилем его посадки на боевой площадке и полосой раздачи консоли к его экрану
входа. Отсюда:

- **сняты** пробы, чей вход — снятый предмет: строки 5 и 11 (архивы поставщика),
  15 (его ручки режима разработки и флаг включения его подчарта), 20 и 21
  (объявления `ui_url:` и полоса раздачи консоли к его экрану), 37 и 38 (тело
  схемы в его секции профиля), 41 и 42 (секреты, которые чеканит его чарт).
  Классы, которые они держали, под нашей посадкой не воспроизводятся: у
  службы доступа ни ручки режима разработки поставщика, ни его секретов, ни его
  секции схем нет, а адреса церемоний обслуживает оболочка консоли (приёмка F8,
  Р1). Возврат поставщика в зонт — находка
  `deploy/own_posture_foreign_identity_test.go` (сторона дерева), его след в
  рендере каждой цепочки — находка
  `deploy/stack_render_carries_no_vendor_residue_test.go`, возврат полосы к
  чужому экрану на цепочке посадки own — находка
  `deploy/tests/helm/console-serves-identity-flows-test.sh`;
- строка 23 остаётся с исходом **снять**, но в этом изменении не снята: её
  предмет — отчёт о происхождении величины обратного вызова, который зовут
  рабочие потоки конвейера (`.github/workflows/*`), — снимается вместе с его
  вызовами, а не раньше них.

## Что эта ведомость НЕ утверждает

- что свойства проб «снять» держатся где-то ещё. Там, где носитель переехал в
  код службы (требование подтверждённого адреса, полосы регистрации), держит ли
  его проба службы — в этом дереве не измерено;
- ничего о пробах вне `deploy/identity_*_test.go` (`ui-future/deploy/identity_*`,
  `gateway/deploy/identity_*` сюда не входят).

## Якоря координат

Координата — номер строки, а номер строки переживает свой предмет молча:
строки, вставленные выше, сдвигают предмет, и координата, оставшаяся в пределах
файла, указывает уже в другое. Поэтому каждая координата этого документа —
строк ведомости, прозы и таблицы выше — прибита **якорем**: текстом, который
стоит на её первой строке, а у диапазона — и на последней. Гейт сверяет якорь со
строкой; якорь не на своей строке — находка, и она называет, где он стоит
теперь. Координата, записанная выше несколько раз, прибита одной строкой.
Правя координату, правьте её якорь тем же изменением.

| координата | первая строка | последняя строка |
|---|---|---|
| `deploy/kaname_hooks_port_follows_posture_test.go:18-20` | `// Прежде порт судился по посадке: слушатель вебхуков процесс поднимал на всякой` | `// и ключа посадки у подчарта нет (kacho#2818): порт хуков запрещён безусловно.` |
| `deploy/identity_bearer_window_ceiling_injection_test.go:18-25` | `func bearerWindowOfProd(t *testing.T, sets ...string) []bearerWindowLifespan {` | `}` |
| `deploy/identity_bearer_window_ceiling_injection_test.go:28` | `func TestMAIL51Injection_LawfulTemplateIsSilent(t *testing.T) {` | — |
| `deploy/identity_bearer_window_ceiling_injection_test.go:36-46` | `func TestMAIL51Injection_RaisedCeilingIsAFinding(t *testing.T) {` | `}` |
| `deploy/identity_bearer_window_ceiling_injection_test.go:50` | `func TestMAIL51Injection_UnclassifiedLifespanIsAFinding(t *testing.T) {` | — |
| `deploy/identity_bearer_window_ceiling_test.go:62-72` | `var bearerWindowRuleset = map[string]bearerWindowRule{` | `}` |
| `deploy/identity_bearer_window_ceiling_test.go:126-157` | `func TestMAIL51BearerWindowCeilingReadsEveryLifespan(t *testing.T) {` | `}` |
| `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:362` | `recovery-code-ttl: {{ .` | — |
| `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:374` | `verification-code-ttl: {{ .` | — |
| `deploy/identity_chart_premise_reachability_test.go:66-97` | `func TestChartPremiseIsActuallyInvoked(t *testing.T) {` | `}` |
| `deploy/identity_chart_premise_reachability_test.go:117-186` | `func TestUmbrellaRenderStillNeedsMaterializedDeps(t *testing.T) {` | `}` |
| `deploy/edge_retired_knobs_render_test.go:4` | `//go:build helmcharts` | — |
| `deploy/identity_chart_premise_reachability_test.go:89-92` | `go test -tags helmcharts ./deploy/...` | `"тег вместе с файлом", ciWorkflow)` |
| `deploy/identity_domainless_landing_injection_test.go:31-39` | `func renderedOwnConfig(t *testing.T, stack string, sets ...string) (string, map[string]any) {` | `}` |
| `deploy/identity_domainless_landing_injection_test.go:41-64` | `func TestIdentityDomainlessGate_ProvenByInjection(t *testing.T) {` | `}` |
| `deploy/identity_domainless_landing_is_expressible_test.go:116-138` | `func judgeOurDomainlessLanding(stack, host string, cfg map[string]any) (domainless bool, findings []string) {` | `}` |
| `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:315` | `cookie-domain: {{ .` | — |
| `deploy/identity_domainless_landing_is_expressible_test.go:60` | `func renderIdentitySubchart(t *testing.T, valueFiles []string, sets ...string) (string, error) {` | — |
| `deploy/client_token_knobs_of_the_pin_test.go:229` | `rendered, err := renderIdentitySubchart(t, nil, sets...)` | — |
| `deploy/identity_global_defaults_agree_test.go:59-116` | `func TestIdentityGlobalDefaultsOfTheSubchartAgreeWithTheUmbrella(t *testing.T) {` | `}` |
| `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:612` | `{{- $mailNode := (((.Values.global).kacho).identity).smtp` | — |
| `deploy/identity_hook_provenance_reaches_every_stand_test.go:57` | `const provReportScript = "scripts/identity-hook-credential-provenance.sh"` | — |
| `deploy/scripts/identity-hook-credential-provenance.sh:5-8` | `# identity-hook-credential-provenance.sh — ТРИ ОТПЕЧАТКА ОДНОЙ ВЕЛИЧИНЫ, снятые` | `# прав). Расхождение любых двух — находка.` |
| `deploy/identity_mail_defaults_are_empty_injection_test.go:26-35` | `func syntheticLayer(smtp map[string]any, hooksScheme string) map[string]any {` | `"smtp":  smtp,` |
| `deploy/identity_mail_defaults_are_empty_injection_test.go:51-56` | `func TestMailDefaultsGateCanStaySilent(t *testing.T) {` | `findings, c := scanMailDefaults(layers)` |
| `deploy/identity_mail_defaults_are_empty_injection_test.go:81` | `func TestMailDefaultsGateCanFail(t *testing.T) {` | — |
| `deploy/identity_mail_defaults_are_empty_test.go:66-72` | `var mailDefaultKnobs = [][]string{` | `}` |
| `deploy/identity_mail_defaults_are_empty_test.go:104-107` | `global.kacho.identity.smtp` | `for _, k := range []string{"global", "kacho", "identity", "smtp"} {` |
| `deploy/identity_mail_defaults_are_empty_test.go:188-215` | `func TestMailDefaultsAreEmpty(t *testing.T) {` | `}` |
| `deploy/identity_mail_defaults_are_empty_test.go:44-45` | `//   - что профиль объявил почтовые величины — это предмет стража рендера (С1) и` | `//     шага подстановки (С2), у каждого своя досягаемость;` |
| `deploy/identity_mail_lane_feeds_both_senders_injection_test.go:21-38` | `func mailFeedCopyTree(t *testing.T) string {` | `}` |
| `deploy/identity_mail_lane_feeds_both_senders_injection_test.go:71-94` | `func TestMAIL48Injection_SenderFedFromAnotherNode(t *testing.T) {` | `}` |
| `deploy/identity_mail_lane_feeds_both_senders_injection_test.go:100-111` | `func TestMAIL48Injection_SenderPartlyFedFromAnotherNode(t *testing.T) {` | `}` |
| `deploy/identity_mail_lane_feeds_both_senders_test.go:55` | `const mailLaneNode = "global.kacho.identity.smtp"` | — |
| `deploy/identity_mail_lane_feeds_both_senders_test.go:262-288` | `func mailLaneFeedFindings(t *testing.T, root string) []string {` | `}` |
| `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:623` | `invite-mail:` | — |
| `deploy/identity_mail_lane_single_declaration_injection_test.go:65` | `tpl:    filepath.Join(root, "charts", "kaname", "templates", "configmap.yaml"),` | — |
| `deploy/identity_mail_lane_single_declaration_injection_test.go:111-219` | `func TestMailLaneGateFailsOnAReturnedDefect(t *testing.T) {` | `}` |
| `deploy/identity_mail_lane_single_declaration_test.go:60` | `const mailLaneFeedPath = "global.kacho.identity.smtp"` | — |
| `deploy/identity_mail_lane_single_declaration_test.go:120-148` | `func ourConfigSections(t *testing.T, tpl string) []string {` | `}` |
| `deploy/identity_second_factor_reachable_injection_test.go:215` | `func TestIdentitySecondFactorInjection_OwnConsoleDeclarationDecidesTheFloor(t *testing.T) {` | — |
| `deploy/identity_second_factor_reachable_injection_test.go:276` | `func TestIdentitySecondFactorInjection_OwnServiceSideIsReadFromThePinnedRoot(t *testing.T) {` | — |
| `deploy/identity_second_factor_reachable_injection_test.go:354` | `func TestIdentitySecondFactorInjection_ExternalLandingIsARefusal(t *testing.T) {` | — |
| `deploy/identity_second_factor_reachable_injection_test.go:51-53` | `"kratos:\n  enabled: true\n  deployment: {}\n",` | `"kratos:\n  enabled: false\n",` |
| `deploy/identity_second_factor_reachable_injection_test.go:115-127` | `const foreignOn = "kratos:\n  enabled: true\nhydra:\n  enabled: true\n"` | `for _, chart := range []string{"kratos", "hydra"} {` |
| `deploy/identity_second_factor_reachable_test.go:26-41` | `// # Стороны 1 и 2 — СВОИ У КАЖДОЙ ПОСАДКИ (#2691)` | `// ровно там, где пол «2» поднять было нечем. Теперь стороны берутся у посадки.` |
| `deploy/identity_second_factor_reachable_test.go:2282-2296` | `func sidesOfLanding(t *testing.T, landing, console string) (secondFactorSides, error) {` | `return ownSecondFactorSides(pin, root, vocab, rule, console)` |
| `deploy/identity_second_factor_reachable_test.go:2332` | `func TestIdentity_SecondFactorReachesTheBrowser(t *testing.T) {` | — |
| `deploy/stand_verdict_carries_provenance_test.go:130` | `var wf provWorkflow` | — |
| `deploy/stand_verdict_carries_provenance_test.go:147` | `for _, recipe := range provStandRecipes {` | — |
