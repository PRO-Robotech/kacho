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
kacho#2818 (раздел «Снятие kacho#2818» ниже).

## Предмет снятия

Снимается **внешний поставщик личности**: подчарты-архивы
`deploy/helm/umbrella/charts/kratos-0.62.1.tgz`, архив его издателя токенов,
каталог `charts/identity-selfservice-ui`, их базы и всё, что существует только ради
их настройки. Четыре файла НАШЕГО подчарта, которые ничего, кроме их
конфигурации, не производили, сняты задачей kacho#2818 — вместе с ключом посадки
службы и полосой хуков, которых пиненная служба больше не читает:

```
deploy/helm/umbrella/charts/kaname/templates/_identity-provider.tpl
deploy/helm/umbrella/charts/kaname/templates/identity-provider-config-configmap.yaml
deploy/helm/umbrella/charts/kaname/templates/identity-provider-hooks-configmap.yaml
deploy/helm/umbrella/charts/kaname/templates/identity-provider-schema-configmap.yaml
```

Остаётся **посадка `own`** — единственная у службы (kaname#363): ключа посадки у
подчарта больше нет, стек поставщика выключен в базе зонта (`values.yaml`,
раздел «ЧУЖОЙ СТЕК ЛИЧНОСТИ НЕ ПОДНИМАЕТСЯ»). Настройки нашей службы рендерит
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
- файлов в предмете на записи #2731: **51**; после снятия kacho#2818 — **24**
  (`git ls-files 'deploy/identity_*_test.go' | wc -l`), строк **9350**; по каждому прочитаны шапка-объявление предмета, источники входа
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
| 2 | `identity_bearer_window_ceiling_test.go` | всякий срок полосы входа, уходящей письмом, отнесён к ограничивающим окно либо к неограничивающим с причиной, и ограничивающий не выше потолка | наша полоса | оставить | перечень `:62-72` судит рендер нашего блока на каждом стеке `:126-157`; сроки кодов объявляет наш шаблон `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:354`, `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:366` |
| 5 | `identity_chart_default_premise_test.go` | умолчание ручки режима разработки в архиве стороннего чарта безопасно | чужая служба | снять | читает архивы поставщика `:111-118` по перечню `deploy/identity_dev_flag_declaration_test.go:78-92`; архивов не станет |
| 6 | `identity_chart_premise_reachability_test.go` | тег `helmcharts` зовётся конвейером, и у его раскола остался предмет | наша полоса | оставить | суд над конвейером и `Chart.yaml` `:66-97`, `:117-186`; под тегом остаётся наша проба `deploy/edge_retired_knobs_render_test.go:4`. Поставщик назван только текстом находки `:89-92` |
| 11 | `identity_courier_arg_premise_test.go` | почтовый процесс чарта поставщика не наследует аргументы основного | чужая служба | снять | читает архив поставщика `:56`, `:119-138`, `:186-187` — факт о чужом дереве |
| 15 | `identity_dev_flag_declaration_test.go` | стенд боевой посадки не держит поставщика в режиме разработки | чужая служба | снять | ручки поставщика `:78-92`, флаг включения его подчарта `:244`, `:268-273` |
| 16 | `identity_domainless_landing_injection_test.go` | гейт выразимости посадки без имени падает на печенье с доменом и на происхождении ключей на IP-литерале и молчит на близнеце | наша полоса | оставить | вход — рендер нашей службы цепочкой a8f60d `:31-39`; вердикт — адъюдикатор гейта строки 17 `:41-64` |
| 17 | `identity_domainless_landing_is_expressible_test.go` | посадка без доменного имени выразима нашей полосой: печенье host-only и ни одного происхождения ключей доступа на IP-литерале | наша полоса | оставить | адъюдикатор `:116-138`; печенье нашей сессии объявляет наш шаблон `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:307`. Помощник рендера `:60` зовут живые пробы (`deploy/client_token_knobs_of_the_pin_test.go:229`) |
| 20 | `identity_flow_path_is_served_injection_test.go` | гейт согласия адресов падает, молчит и отличает «не обслуживается» от «неразрешимо» | чужая служба | снять | синтетика на шаблоне поставщика `:145`, `:243`; уходит с гейтом строки 21 |
| 21 | `identity_flow_path_is_served_test.go` | адрес потока, выдаваемый браузеру службой личности, обслуживается раздачей консоли | чужая служба | снять | обе половины — поставщика: объявления `ui_url:` `:839`, `:904-906` и полоса раздачи к его экрану `:10-15`, `:88`; экраны входа ведёт консоль (приёмка F8), и полоса снимается вместе с объявлениями (#1276, п. 3) |
| 22 | `identity_global_defaults_agree_test.go` | копия `global.kacho.identity` в подчарте совпадает с зонтом | наша полоса | оставить | согласие двух объявлений `:59-116` судит наш узел, который читает наш отправитель `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:604`; проверка адреса хуков снята вместе с предметом (kacho#2818) |
| 23 | `identity_hook_provenance_reaches_every_stand_test.go` | работа, поднимающая стенд, собирает отчёт о происхождении величины обратного вызова | чужая служба | снять | отчёт `:57` снимает отпечатки у отправителя-поставщика и проверяющей стороны `deploy/scripts/identity-hook-credential-provenance.sh:5-8`; отправителя не станет |
| 26 | `identity_mail_defaults_are_empty_injection_test.go` | гейт MAIL-12 падает и молчит на синтетическом слое | наша полоса | оставить | синтетика строится вокруг нашего узла `:26-35` и зовёт вердикт гейта строки 27 `:51-56`, `:81` |
| 27 | `identity_mail_defaults_are_empty_test.go` | у пяти почтовых величин `global.kacho.identity.smtp` нет встроенного умолчания ни в одном слое | наша полоса | оставить | перечень ручек нашего узла `:66-72`, обход слоёв умолчаний `:104-107`, `:188-215`; узел читает наш отправитель `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:604`. Поставщик назван только в границе `:44-45` |
| 28 | `identity_mail_lane_feeds_both_senders_injection_test.go` | MAIL-48 падает на отправителе, питаемом чужим узлом целиком и частично, и на снятом разделе, молчит на законной копии | наша полоса | оставить | копия НАШЕГО шаблона `:21-38`; инъекции `:71-94`, `:100-111` |
| 29 | `identity_mail_lane_feeds_both_senders_test.go` | наш отправитель письма питается ровно узлом `global.kacho.identity.smtp` | наша полоса | оставить | узел решения Р23 `:55`, вердикт `:262-288`; раздел отправителя — наш шаблон `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:615`. Второго отправителя больше нет: настройки поставщика подчарт не производит (kacho#2818) |
| 30 | `identity_mail_lane_single_declaration_injection_test.go` | MAIL-54 падает только на своём предмете по трём осям | наша полоса | оставить | копия дерева с НАШИМ шаблоном `:65`; три оси с близнецами `:105-210` |
| 31 | `identity_mail_lane_single_declaration_test.go` | почтовая полоса объявлена одним местом — разделом нашего отправителя, — и оснастка раскатки посылает оператора туда | наша полоса | оставить | координата питания `:60`; разделы наших настроек выводятся из шаблона `:120-148`; единственное объявление — `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:615` |
| 37 | `identity_schema_body_single_node_injection_test.go` | гейт единственного узла тела схемы падает по обеим формам записи | чужая служба | снять | все входы — ключи подчарта поставщика `:54-77` |
| 38 | `identity_schema_body_single_node_test.go` | тело схемы личности объявлено в профиле одним узлом | чужая служба | снять | две стороны — наша секция унаследованных схем и секция поставщика `:11-16`, `:265`; читатель унаследованных схем — шаблон поставщика в подчарте службы — снят (kacho#2818) |
| 39 | `identity_second_factor_reachable_injection_test.go` | гейт достижимости второго фактора падает и молчит по каждой стороне | наша полоса | оставить | стороны `own` — объявление консоли и корень пиненной службы `:215`, `:276`; посадка `external` — отказ `:354`. Поставщик назван синтетикой `:51-53`, `:115-127`; самопроверка предиката монтирования ушла вместе с ним (строка 40) |
| 40 | `identity_second_factor_reachable_test.go` | пол уровня уверенности «2» достижим: служба, консоль и каталог прав сходятся | наша полоса | оставить | стороны посадки `own` `:26-41`, `:2282-2296`, `:2332`; путь настроек поставщика и предикат монтирования ушли вместе с пробами строк 14 и 35 (kacho#2818), половина службы — одна посадка, `own` (`kanameLanding`) — предмет файла от этого не меняется |
| 41 | `identity_session_secret_source_injection_test.go` | гейт называет недостающую величину сессии и не требует секрета от профиля извне git | чужая служба | снять | синтетика деревьев подчарта поставщика `:28-43` |
| 42 | `identity_session_secret_source_test.go` | самодостаточный в git стенд объявляет величины сессии службы личности | чужая служба | снять | предмет — секреты, которые чарт поставщика чеканит на каждом рендере `:10-16`, `:191-208` |

## Разбивка по исходам

| исход | файлов |
|---|---:|
| снять | 10 |
| оставить | 14 |
| переписать | 0 |
| **итого** | **24** |

Сумма равна числу файлов; файлов без исхода нет, исходов вне словаря нет. Сверка
имён ведомости с деревом, исходов, координат и их якорей — прогоном гейта:

```sh
go test ./internal/repohygiene/ -run 'TestIdentityProbeFate' -count=1 -v
# перепись: строк ведомости 24 · файлов в дереве 24 · снять 10 · оставить 14 · переписать 0 · …
```

## Снимаемое, на котором держится живое

Проба с исходом «снять» объявляет помощники, которые читают ЖИВЫЕ пробы вне этой
ведомости. Снять файл целиком нельзя: помощник обязан переехать к читателю тем
же изменением, иначе пакет не соберётся.

| снимаемая проба | помощник | живой читатель |
|---|---|---|
| `identity_dev_flag_declaration_test.go` | `declaresProduction` | `deploy/db_footprint_declaration_test.go:206` |
| `identity_hook_provenance_reaches_every_stand_test.go` | `provWorkflow`, `provStandRecipes` | `deploy/stand_verdict_carries_provenance_test.go:130`, `deploy/stand_verdict_carries_provenance_test.go:147` |

Снятые задачей kacho#2818 пробы унесли своих помощников к читателям тем же
изменением: `scalarLine` — в `deploy/rollout_preflight_covers_required_secrets_test.go`,
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
| `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:354` | `recovery-code-ttl: {{ .` | — |
| `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:366` | `verification-code-ttl: {{ .` | — |
| `deploy/identity_chart_default_premise_test.go:111-118` | `func TestIdentityDevFlags_ChartDefaultsAreStillSecure(t *testing.T) {` | `vals := chartArchiveValues(t, k.archive)` |
| `deploy/identity_dev_flag_declaration_test.go:78-92` | `func identityDevKnobs() []identityDevKnob {` | `}` |
| `deploy/identity_chart_premise_reachability_test.go:66-97` | `func TestChartPremiseIsActuallyInvoked(t *testing.T) {` | `}` |
| `deploy/identity_chart_premise_reachability_test.go:117-186` | `func TestUmbrellaRenderStillNeedsMaterializedDeps(t *testing.T) {` | `}` |
| `deploy/edge_retired_knobs_render_test.go:4` | `//go:build helmcharts` | — |
| `deploy/identity_chart_premise_reachability_test.go:89-92` | `go test -tags helmcharts ./deploy/...` | `"тег вместе с файлом", ciWorkflow)` |
| `deploy/identity_courier_arg_premise_test.go:56` | `const identityProviderArchiveGlob = "kratos-*.tgz"` | — |
| `deploy/identity_courier_arg_premise_test.go:119-138` | `func TestCourierArgsAreNotInheritedFromTheDeployment(t *testing.T) {` | `t.Errorf("%s: шаблон почтового процесса СТАЛ читать` |
| `deploy/identity_courier_arg_premise_test.go:186-187` | `serveBody, serveMember := chartArchiveMember(t, archive, "deployment-kratos.yaml")` | `(?m)^\s*-\s*/etc/config/kratos\.yaml\s*$` |
| `deploy/identity_dev_flag_declaration_test.go:244` | `on, ok := lookup(f.effective, k.subchart, "enabled")` | — |
| `deploy/identity_dev_flag_declaration_test.go:268-273` | `if enabled == 0 && flagRead != prodStacks*len(knobs) {` | `}` |
| `deploy/identity_domainless_landing_injection_test.go:31-39` | `func renderedOwnConfig(t *testing.T, stack string, sets ...string) (string, map[string]any) {` | `}` |
| `deploy/identity_domainless_landing_injection_test.go:41-64` | `func TestIdentityDomainlessGate_ProvenByInjection(t *testing.T) {` | `}` |
| `deploy/identity_domainless_landing_is_expressible_test.go:116-138` | `func judgeOurDomainlessLanding(stack, host string, cfg map[string]any) (domainless bool, findings []string) {` | `}` |
| `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:307` | `cookie-domain: {{ .` | — |
| `deploy/identity_domainless_landing_is_expressible_test.go:60` | `func renderIdentitySubchart(t *testing.T, valueFiles []string, sets ...string) (string, error) {` | — |
| `deploy/client_token_knobs_of_the_pin_test.go:229` | `rendered, err := renderIdentitySubchart(t, nil, sets...)` | — |
| `deploy/identity_flow_path_is_served_injection_test.go:145` | `return flowDecl{file: "_identity-provider.tpl", raw: "{{ $flow }}/registration", vars: identityTemplateVars(prefixKnob)}` | — |
| `deploy/identity_flow_path_is_served_injection_test.go:243` | `file: "_identity-provider.tpl", raw: "{{ $flow }}/login",` | — |
| `deploy/identity_flow_path_is_served_test.go:839` | `if !strings.Contains(body, "ui_url:") {` | — |
| `deploy/identity_flow_path_is_served_test.go:904-906` | `if decls == 0 {` | `"и «ноль находок» здесь означало бы «ноль прочитанного»", helmDeclarationsRoot)` |
| `deploy/identity_flow_path_is_served_test.go:10-15` | `// Об одном предмете говорят ДВА места, и они разошлись. Служба личности` | `// ничем.` |
| `deploy/identity_flow_path_is_served_test.go:88` | `const nginxServingChart = "../ui-future/deploy/templates/configmap-nginx.yaml"` | — |
| `deploy/identity_global_defaults_agree_test.go:59-116` | `func TestIdentityGlobalDefaultsOfTheSubchartAgreeWithTheUmbrella(t *testing.T) {` | `}` |
| `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:604` | `{{- $mailNode := (((.Values.global).kacho).identity).smtp` | — |
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
| `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:615` | `invite-mail:` | — |
| `deploy/identity_mail_lane_single_declaration_injection_test.go:65` | `tpl:    filepath.Join(root, "charts", "kaname", "templates", "configmap.yaml"),` | — |
| `deploy/identity_mail_lane_single_declaration_injection_test.go:105-210` | `func TestMailLaneGateFailsOnAReturnedDefect(t *testing.T) {` | `}` |
| `deploy/identity_mail_lane_single_declaration_test.go:60` | `const mailLaneFeedPath = "global.kacho.identity.smtp"` | — |
| `deploy/identity_mail_lane_single_declaration_test.go:120-148` | `func ourConfigSections(t *testing.T, tpl string) []string {` | `}` |
| `deploy/identity_schema_body_single_node_injection_test.go:54-77` | `"kratos:\n  identitySchemas:\n    \"identity.default.schema.json\":` | `wantPathSub: "kratos.identitySchemas",` |
| `deploy/identity_schema_body_single_node_test.go:11-16` | `// Тело схемы личности нужно ДВУМ сторонам настроек сразу: нашей секции (через` | `// строкам, решит порядок слияния, а не решение.` |
| `deploy/identity_schema_body_single_node_test.go:265` | `func TestIdentitySchemaBodyIsDeclaredByASingleNode(t *testing.T) {` | — |
| `deploy/identity_second_factor_reachable_injection_test.go:215` | `func TestIdentitySecondFactorInjection_OwnConsoleDeclarationDecidesTheFloor(t *testing.T) {` | — |
| `deploy/identity_second_factor_reachable_injection_test.go:276` | `func TestIdentitySecondFactorInjection_OwnServiceSideIsReadFromThePinnedRoot(t *testing.T) {` | — |
| `deploy/identity_second_factor_reachable_injection_test.go:354` | `func TestIdentitySecondFactorInjection_ExternalLandingIsARefusal(t *testing.T) {` | — |
| `deploy/identity_second_factor_reachable_injection_test.go:51-53` | `"kratos:\n  enabled: true\n  deployment: {}\n",` | `"kratos:\n  enabled: false\n",` |
| `deploy/identity_second_factor_reachable_injection_test.go:115-127` | `const foreignOn = "kratos:\n  enabled: true\nhydra:\n  enabled: true\n"` | `for _, chart := range []string{"kratos", "hydra"} {` |
| `deploy/identity_second_factor_reachable_test.go:26-41` | `// # Стороны 1 и 2 — СВОИ У КАЖДОЙ ПОСАДКИ (#2691)` | `// ровно там, где пол «2» поднять было нечем. Теперь стороны берутся у посадки.` |
| `deploy/identity_second_factor_reachable_test.go:2282-2296` | `func sidesOfLanding(t *testing.T, landing, console string) (secondFactorSides, error) {` | `return ownSecondFactorSides(pin, root, vocab, rule, console)` |
| `deploy/identity_second_factor_reachable_test.go:2332` | `func TestIdentity_SecondFactorReachesTheBrowser(t *testing.T) {` | — |
| `deploy/identity_session_secret_source_injection_test.go:28-43` | `func lane(dsn string, secrets map[string]any, secret map[string]any) map[string]any {` | `return map[string]any{"kratos": k}` |
| `deploy/identity_session_secret_source_test.go:10-16` | `kratos.kratos.config.secrets.{default,cookie,cipher}` | `// сессии арендаторов.` |
| `deploy/identity_session_secret_source_test.go:191-208` | `kratos, _ := values["kratos"].(map[string]any)` | `inner, _ := kratos["kratos"].(map[string]any)` |
| `deploy/db_footprint_declaration_test.go:206` | `prod, why := declaresProduction(declared)` | — |
| `deploy/stand_verdict_carries_provenance_test.go:130` | `var wf provWorkflow` | — |
| `deploy/stand_verdict_carries_provenance_test.go:147` | `for _, recipe := range provStandRecipes {` | — |
