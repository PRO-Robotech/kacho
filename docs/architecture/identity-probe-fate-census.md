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

## Предмет снятия

Снимается **внешний поставщик личности**: подчарты-архивы
`deploy/helm/umbrella/charts/kratos-0.62.1.tgz`, архив его издателя токенов,
каталог `charts/identity-selfservice-ui`, их базы и всё, что существует только ради
их настройки, — включая четыре файла НАШЕГО подчарта, которые ничего, кроме их
конфигурации, не производят:

```
deploy/helm/umbrella/charts/kaname/templates/_identity-provider.tpl
deploy/helm/umbrella/charts/kaname/templates/identity-provider-config-configmap.yaml
deploy/helm/umbrella/charts/kaname/templates/identity-provider-hooks-configmap.yaml
deploy/helm/umbrella/charts/kaname/templates/identity-provider-schema-configmap.yaml
```

Остаётся **посадка `own`**: её объявляют корни всех цепочек `deploy/stacks.txt`, а
стек поставщика выключен в базе зонта (`values.yaml`, раздел «ЧУЖОЙ СТЕК ЛИЧНОСТИ
НЕ ПОДНИМАЕТСЯ»). Настройки нашей службы рендерит
`charts/kaname/templates/configmap.yaml`; слушатель хуков поставщика служба под
`own` не строит (`deploy/kaname_hooks_port_follows_posture_test.go:11-13`).

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
- файлов в предмете: **51** (`git ls-files 'deploy/identity_*_test.go' | wc -l`), строк
  **18 834**; по каждому прочитаны шапка-объявление предмета, источники входа
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
| 1 | `identity_bearer_window_ceiling_injection_test.go` | потолок сроков предъявителей падает и молчит на копии шаблона | чужая служба | переписать | правит копию тела шаблона поставщика `:18-26`, `:51-53`; переезжает вместе с гейтом строки 2 |
| 2 | `identity_bearer_window_ceiling_test.go` | всякий срок предъявителя в настройках личности отнесён к ограничивающим окно либо к неограничивающим с причиной, и ограничивающий не выше потолка | чужая служба | переписать | перечень адресует ключи поставщика `:77-100` и читает его шаблон `:244`; код восстановления под `own` чеканим мы, его срок объявляет наш шаблон `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:373-379` — потолок обязан судить его |
| 3 | `identity_callback_credential_source_test.go` | величина обратного вызова у отправителя и проверяющей стороны приходит из одного секрета | чужая служба | снять | отправитель — поставщик и его издатель `:11-15`, полоса узнаётся по маршруту хуков `:97`, словарь полос — хуки издателя `deploy/helm/umbrella/templates/identity-callback-credential-guard.yaml:69`; отправителя не станет, и пары нет |
| 4 | `identity_callback_transport_test.go` | транспорт обратного вызова к слушателю хуков — решение профиля и совпадает со слушателем | чужая служба | снять | полосы — по маршруту хуков `:103`, `:458-475`; все их объявляет поставщик (`deploy/helm/umbrella/charts/kaname/templates/_identity-provider.tpl:391`, `deploy/helm/umbrella/values.prod.yaml:2038`), а слушатель хуков под `own` не строится `deploy/kaname_hooks_port_follows_posture_test.go:11-13` |
| 5 | `identity_chart_default_premise_test.go` | умолчание ручки режима разработки в архиве стороннего чарта безопасно | чужая служба | снять | читает архивы поставщика `:111-118` по перечню `deploy/identity_dev_flag_declaration_test.go:78-92`; архивов не станет |
| 6 | `identity_chart_premise_reachability_test.go` | тег `helmcharts` зовётся конвейером, и у его раскола остался предмет | наша полоса | оставить | суд над конвейером и `Chart.yaml` `:66-97`, `:117-186`; под тегом остаётся наша проба `deploy/edge_retired_knobs_render_test.go:4`. Поставщик назван только текстом находки `:89-92` |
| 7 | `identity_config_digest_binds_the_same_text_test.go` | отпечаток в шаблоне пода считается по тому же тексту, из которого рендерится карта настроек | чужая служба | снять | форма «ключ карты рендерится именованным шаблоном» `:45` накрывает ровно три карты поставщика (`deploy/helm/umbrella/charts/kaname/templates/identity-provider-config-configmap.yaml:89`, `deploy/helm/umbrella/charts/kaname/templates/identity-provider-hooks-configmap.yaml:48`, `deploy/helm/umbrella/charts/kaname/templates/identity-provider-schema-configmap.yaml:28`) и их отпечатки в профилях `deploy/helm/umbrella/values.prod.yaml:1771-1775`; наша карта связана отпечатком по построению `deploy/helm/umbrella/charts/kaname/templates/deployment.yaml:89`. После снятия обход пуст `:91-95` |
| 8 | `identity_config_mount_census_test.go` | число монтирующих профилей, объявленное стражем, равно выведенному обходом | чужая служба | снять | цепочка вывода идёт от довода `--config` процесса поставщика через шаг подстановки в его под `:43-51`, `:169`; ни шага, ни карты не останется |
| 9 | `identity_config_reaches_the_process_test.go` | карта настроек, несущая маршрут обратных вызовов, где-нибудь смонтирована | чужая служба | снять | предмет — карта с маршрутом хуков поставщика `:27-30`, `:69`, `:111`; маршрут уходит вместе с его хуками |
| 10 | `identity_config_template_test.go` | общая координата объявления настроек службы личности для проб пакета | чужая служба | переписать | указывает в шаблон поставщика `:19-22` и в его ключ схемы `:24-26`; читатели, которые переписываются (строки 2, 17, 29, 31), нуждаются в координате НАШЕГО объявления — она уже названа `deploy/identity_mail_lane_feeds_both_senders_test.go:53-54` |
| 11 | `identity_courier_arg_premise_test.go` | почтовый процесс чарта поставщика не наследует аргументы основного | чужая служба | снять | читает архив поставщика `:54`, `:117-136`, `:184-185` — факт о чужом дереве |
| 12 | `identity_courier_reads_what_it_mounts_test.go` | основной и почтовый процессы поставщика читают один набор файлов настроек | чужая служба | снять | два рабочих объекта поставщика и их ручки `:10-18`, `:99`, `:178-179`; у нашей службы второго процесса нет |
| 13 | `identity_delivery_flows_declared_once_injection_test.go` | MAIL-18 краснеет на возвращённом дефекте и молчит на близнеце | чужая служба | снять | копия зонта и вердикт гейта строки 14 `:27`, `:33`, `:54` |
| 14 | `identity_delivery_flows_declared_once_test.go` | профиль не заводит второго мнения о потоках доставки и почтовой полосе | чужая служба | снять | второе мнение ищется в разделе поставщика `:65-70` и только у цепочек, доводящих наши настройки до его процесса `:111-122`; наша полоса объявлена одним блоком шаблона `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:318-405`. После снятия монтирующих цепочек ноль, а отказ стоит только на нуле посадок `:154-159` — проба позеленела бы беспредметно |
| 15 | `identity_dev_flag_declaration_test.go` | стенд боевой посадки не держит поставщика в режиме разработки | чужая служба | снять | ручки поставщика `:78-92`, флаг включения его подчарта `:244`, `:268-273` |
| 16 | `identity_domainless_landing_injection_test.go` | гейт выразимости посадки без имени падает и молчит по четырём осям | чужая служба | переписать | зовёт функции вердикта гейта строки 17 `:12-13`, `:56`, `:69`; переезжает вместе с ним |
| 17 | `identity_domainless_landing_is_expressible_test.go` | посадка без доменного имени выразима, объявление согласно с фактом в обе стороны | чужая служба | переписать | вердикт читает ключ карты поставщика `:88-114` и его ключ печенья `:123-129`; печенье сессии под `own` объявляет наш шаблон `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:331-332`, и посадка на IP-литерале обязана оставаться выразимой у него. Помощники `:60`, `:68` читают живые пробы (`deploy/client_token_knobs_of_the_pin_test.go:229`) |
| 18 | `identity_file_keys_survive_the_environment_injection_test.go` | гейт перебивания ключа окружением падает и молчит | чужая служба | снять | инъекция идёт ручкой чарта поставщика `:12-15`, `:126` |
| 19 | `identity_file_keys_survive_the_environment_test.go` | на `own` в рендере нет объектов поставщика; на поднятом пробой поставщике наш файл не перебивается окружением его процесса | чужая служба | снять | обе половины — о поставщике: отсутствие его объектов `:102-116` и его процесс, поднятый пробой `:131-135`; после физического снятия первое держит потолок привязок `internal/repohygiene/retiredidentityvendorceiling.go:6-7`, второму судить некого |
| 20 | `identity_flow_path_is_served_injection_test.go` | гейт согласия адресов падает, молчит и отличает «не обслуживается» от «неразрешимо» | чужая служба | снять | синтетика на шаблоне поставщика `:145`, `:243`; уходит с гейтом строки 21 |
| 21 | `identity_flow_path_is_served_test.go` | адрес потока, выдаваемый браузеру службой личности, обслуживается раздачей консоли | чужая служба | снять | обе половины — поставщика: объявления `ui_url:` `:839`, `:904-906` и полоса раздачи к его экрану `:10-15`, `:88`; экраны входа ведёт консоль (приёмка F8), и полоса снимается вместе с объявлениями (#1276, п. 3) |
| 22 | `identity_global_defaults_agree_test.go` | копия `global.kacho.identity` в подчарте совпадает с зонтом; умолчания адреса хуков согласны с подчартом | наша полоса | переписать | согласие двух объявлений `:71-128` судит наш узел, который читает наш отправитель `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:640`, и остаётся; довод «значения доходят до подчарта поставщика» `:10-13` умирает, а проверка адреса хуков `:134-168` теряет предмет вместе с ними |
| 23 | `identity_hook_provenance_reaches_every_stand_test.go` | работа, поднимающая стенд, собирает отчёт о происхождении величины обратного вызова | чужая служба | снять | отчёт `:57` снимает отпечатки у отправителя-поставщика и проверяющей стороны `deploy/scripts/identity-hook-credential-provenance.sh:5-8`; отправителя не станет |
| 24 | `identity_inherited_schemas_are_declared_test.go` | наша секция схем личности покрывает схемы, объявленные слоем под нами | чужая служба | снять | прокси прямо назван — единственное объявление кроме нашего есть секция подчарта поставщика `:30-34`; слоя под нами не останется |
| 25 | `identity_inherited_schemas_injection_test.go` | гейт покрытия схем падает и молчит по пяти осям | чужая служба | снять | зовёт вердикт гейта строки 24 `:47`, `:65` |
| 26 | `identity_mail_defaults_are_empty_injection_test.go` | гейт MAIL-12 падает и молчит на синтетическом слое | наша полоса | оставить | синтетика строится вокруг нашего узла `:26-35` и зовёт вердикт гейта строки 27 `:51-56`, `:81` |
| 27 | `identity_mail_defaults_are_empty_test.go` | у пяти почтовых величин `global.kacho.identity.smtp` нет встроенного умолчания ни в одном слое | наша полоса | оставить | перечень ручек нашего узла `:66-72`, обход слоёв умолчаний `:104-107`, `:188-215`; узел читает наш отправитель `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:640`. Поставщик назван только в границе `:44-45` |
| 28 | `identity_mail_lane_feeds_both_senders_injection_test.go` | MAIL-48 падает в обе стороны на копиях обоих шаблонов | чужая служба | переписать | правит копии шаблона поставщика и нашего `:24`, `:60-62`, `:71-79`; переезжает вместе с гейтом строки 29 |
| 29 | `identity_mail_lane_feeds_both_senders_test.go` | оба отправителя письма питаются из одного узла значений | чужая служба | переписать | пара — почтовый процесс поставщика и наш отправитель `:6-11`, `:277-278`, `:314`; отправитель остаётся один `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:640`, и утверждение обязано стать безусловным — наш отправитель питается из `global.kacho.identity.smtp`. Выкинуть половину пары и оставить зелёное — ослабление |
| 30 | `identity_mail_lane_single_declaration_injection_test.go` | MAIL-54 падает только на своём предмете по трём осям | чужая служба | переписать | копия дерева с шаблоном поставщика `:29`, `:65`, `:105`; переезжает вместе с гейтом строки 31 |
| 31 | `identity_mail_lane_single_declaration_test.go` | почтовая полоса объявлена одним местом, и оснастка раскатки посылает оператора туда | чужая служба | переписать | координата питания наша `:56` и остаётся; единица переписи — раздел формы поставщика `:75-80`, `:237-253`. Единственность обязана судить объявление НАШЕЙ почтовой секции `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:640` |
| 32 | `identity_method_comment_matches_declaration_injection_test.go` | гейт перечня полос входа краснеет с именем полосы и молчит на прозе | чужая служба | снять | правит копию шаблона поставщика `:29`, `:37-39` |
| 33 | `identity_method_comment_matches_declaration_test.go` | отмеченный перечень полос входа согласен с разобранным объявлением | чужая служба | снять | авторитет — блок методов поставщика `:20-25`, `:230-243`; отмеченный перечень есть только в `deploy/helm/umbrella/charts/kaname/templates/_identity-provider.tpl:350-351`, наш шаблон методов не объявляет |
| 34 | `identity_registration_lanes_issue_a_session_test.go` | каждая полоса регистрации выдаёт сессию | чужая служба | снять | читает хуки полос регистрации поставщика `:44-72`, `:103-104`; наш блок регистрации полос не несёт `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:419-425` |
| 35 | `identity_replaced_lists_are_decided_test.go` | список, объявленный обеими сторонами настроек, несёт явное решение | чужая служба | снять | механизм — слияние двух файлов настроек процессом поставщика `:10-15`, `:94-102`, `:698`; у нашей службы второй стороны нет |
| 36 | `identity_replaced_lists_injection_test.go` | гейт замещаемых списков падает по вердикту и четырём формам записи | чужая служба | снять | зовёт вердикт гейта строки 35 `:9-10`, `:34` |
| 37 | `identity_schema_body_single_node_injection_test.go` | гейт единственного узла тела схемы падает по обеим формам записи | чужая служба | снять | все входы — ключи подчарта поставщика `:54-77` |
| 38 | `identity_schema_body_single_node_test.go` | тело схемы личности объявлено в профиле одним узлом | чужая служба | снять | две стороны — наша секция унаследованных схем и секция поставщика `:11-16`, `:265`; унаследованные схемы читает только шаблон поставщика `deploy/helm/umbrella/charts/kaname/templates/_identity-provider.tpl:198` |
| 39 | `identity_second_factor_reachable_injection_test.go` | гейт достижимости второго фактора падает и молчит по каждой стороне | наша полоса | оставить | стороны `own` — объявление консоли и корень пиненной службы `:226`, `:287`; посадка `external` — отказ `:365`. Поставщик назван синтетикой `:61-63`, `:126-138`; самопроверка предиката монтирования `:29-37` уходит вместе с ним (строка 40) |
| 40 | `identity_second_factor_reachable_test.go` | пол уровня уверенности «2» достижим: служба, консоль и каталог прав сходятся | наша полоса | оставить | стороны посадки `own` `:26-41`, `:2242-2256`, `:2292`; путь настроек поставщика `:171` и предикат монтирования `:2381-2388` служат пробам строк 14 и 35 и уходят вместе с ними — предмет файла от этого не меняется |
| 41 | `identity_session_secret_source_injection_test.go` | гейт называет недостающую величину сессии и не требует секрета от профиля извне git | чужая служба | снять | синтетика деревьев подчарта поставщика `:28-43` |
| 42 | `identity_session_secret_source_test.go` | самодостаточный в git стенд объявляет величины сессии службы личности | чужая служба | снять | предмет — секреты, которые чарт поставщика чеканит на каждом рендере `:10-16`, `:191-208` |
| 43 | `identity_step_declaration_parses_injection_test.go` | гейт разбора шага падает на склейке строк и молчит на близнеце | чужая служба | снять | находка обязана называть шаблон поставщика `:83`, `:109` |
| 44 | `identity_step_declaration_parses_test.go` | объявление шага-контейнера разбирается как YAML, имена окружения объявлены однажды | чужая служба | снять | популяция по признаку `:116-119` — ровно одно объявление, шаг подстановки в под поставщика `deploy/helm/umbrella/charts/kaname/templates/_identity-provider.tpl:782`; после снятия обход пуст `:190-192` |
| 45 | `identity_step_survives_kubernetes_expansion_injection_test.go` | гейт подстановки падает по двум формам и молчит на двух близнецах | чужая служба | снять | находка обязана называть шаблон поставщика `:49`, `:69` |
| 46 | `identity_step_survives_kubernetes_expansion_test.go` | текст `command`/`args` шага доезжает до оболочки дословно | чужая служба | снять | та же популяция `:52-54`, замер снят на образе поставщика `:15-24`, `:33`, `:208` |
| 47 | `identity_substitution_judges_the_form_test.go` | шаг подстановки судит остаток по форме ссылки, отказ закрыт | чужая служба | снять | предмет — тот же шаг подстановки в настройки поставщика `:10-14`, `:203-204`, `:216-218` |
| 48 | `identity_verification_mirrors_the_requirement_injection_test.go` | MAIL-16 падает по четырём формам записи и судит сумму, а не файл | чужая служба | снять | копия дерева и вердикт гейта строки 49 `:46`, `:90` |
| 49 | `identity_verification_mirrors_the_requirement_test.go` | требование подтверждённого адреса и включённость потока подтверждения зеркальны на наборе стенда | чужая служба | снять | обе половины читаются из шаблона поставщика и профилей поверх него `:9-12`, `:70`, `:291-293`, `:356`; наш шаблон ни требования, ни потока не объявляет — свойство под `own` принадлежит коду службы |
| 50 | `identity_verified_address_required_on_both_lanes_injection_test.go` | MAIL-52 падает: снятие требования — находка с именем полосы | чужая служба | снять | правит копию шаблона поставщика `:23-26` |
| 51 | `identity_verified_address_required_on_both_lanes_test.go` | требование подтверждённого адреса стоит на каждой полосе входа | чужая служба | снять | хук требования `:54`, `:169-170` стоит только в шаблоне поставщика `deploy/helm/umbrella/charts/kaname/templates/_identity-provider.tpl:446`, `deploy/helm/umbrella/charts/kaname/templates/_identity-provider.tpl:461`; носителя в нашем шаблоне нет |

## Разбивка по исходам

| исход | файлов |
|---|---:|
| снять | 36 |
| оставить | 5 |
| переписать | 10 |
| **итого** | **51** |

Сумма равна числу файлов; файлов без исхода нет, исходов вне словаря нет. Сверка
имён ведомости с деревом, исходов, координат и их якорей — прогоном гейта:

```sh
go test ./internal/repohygiene/ -run 'TestIdentityProbeFate' -count=1 -v
# перепись: строк ведомости 51 · файлов в дереве 51 · снять 36 · оставить 5 · переписать 10 · …
```

## Снимаемое, на котором держится живое

Проба с исходом «снять» объявляет помощники, которые читают ЖИВЫЕ пробы вне этой
ведомости. Снять файл целиком нельзя: помощник обязан переехать к читателю тем
же изменением, иначе пакет не соберётся.

| снимаемая проба | помощник | живой читатель |
|---|---|---|
| `identity_callback_credential_source_test.go` | `scalarLine` | `deploy/rollout_preflight_covers_required_secrets_test.go:243` |
| `identity_courier_reads_what_it_mounts_test.go` | `contains` | `deploy/kaname_listener_knobs_test.go:132`, `deploy/own_ceilings_and_access_keys_umbrella_test.go:269`, `deploy/trust_anchor_claim_matches_declaration_test.go:245` |
| `identity_dev_flag_declaration_test.go` | `declaresProduction` | `deploy/db_footprint_declaration_test.go:206` |
| `identity_file_keys_survive_the_environment_test.go` | `renderStack`, `decodeRender`, `str`, `submap`, `slice` | `deploy/edge_retired_knobs_render_test.go:57-66` |
| `identity_hook_provenance_reaches_every_stand_test.go` | `provStandRecipes`, `provWorkflow` | `deploy/stand_verdict_carries_provenance_test.go:130`, `deploy/stand_verdict_carries_provenance_test.go:147` |
| `identity_inherited_schemas_are_declared_test.go` | `mergedValuesOfStack` | `deploy/own_minting_census_test.go:556`, `deploy/own_posture_foreign_identity_test.go:365` |

Помощники переписываемых и оставляемых проб, которые читают живые пробы
(`renderIdentitySubchart`, `iamSubchartDir`, `leafString`, `identityLanding*`),
остаются на месте вместе со своими файлами.

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

## Что эта ведомость НЕ утверждает

- что перевод уже исполним: у переписываемых проб носитель назван, но гейт на
  нём ещё не заведён — это работа снимающих изменений (#1276, #2818), а не
  переименование константы;
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
| `deploy/kaname_hooks_port_follows_posture_test.go:11-13` | `// Служба с ревизии, несущей вливание kaname#360, под посадкой` | `// корне отдаёт пусто), а` |
| `deploy/identity_bearer_window_ceiling_injection_test.go:18-26` | `// bearerWindowBody — тело действующего шаблона.` | `}` |
| `deploy/identity_bearer_window_ceiling_injection_test.go:51-53` | `body := bearerWindowEdit(t, bearerWindowBody(t),` | `"      lifespan: 72h\n      use: code")` |
| `deploy/identity_bearer_window_ceiling_test.go:77-100` | `var bearerWindowRuleset = map[string]bearerWindowRule{` | `}` |
| `deploy/identity_bearer_window_ceiling_test.go:244` | `raw, err := os.ReadFile(identityConfigTemplate)` | — |
| `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:373-379` | `{{- /* ВОССТАНОВЛЕНИЕ ДОСТУПА на той же полосе (Ф5, kacho#2701): срок кода` | `recovery-code-ttl: {{ .` |
| `deploy/identity_callback_credential_source_test.go:11-15` | `// Полосу обратного вызова исполняют два разных пода. Отправитель (провайдер` | `// быть ОДНОЙ.` |
| `deploy/identity_callback_credential_source_test.go:97` | `const callbackRoute = "/iam/v1/hooks/"` | — |
| `deploy/helm/umbrella/templates/identity-callback-credential-guard.yaml:69` | `{{- $lanes := dict "token_hook" "OAUTH2_TOKEN_HOOK_AUTH_CONFIG_VALUE" "refresh_token_hook" "OAUTH2_REFRESH_TOKEN_HOOK_AUTH_CONFIG_VALUE" }}` | — |
| `deploy/identity_callback_transport_test.go:103` | `const hooksRoute = "/iam/v1/hooks/"` | — |
| `deploy/identity_callback_transport_test.go:458-475` | `func TestIdentityCallbacks_TransportIsProfileDecidedAndMatchesTheListener(t *testing.T) {` | `}` |
| `deploy/helm/umbrella/charts/kaname/templates/_identity-provider.tpl:391` | `url: {{ .Values.global.kacho.identity.hooks.scheme }}://{{ include "kacho.identity.hooksAuthority" . }}/iam/v1/hooks/provision` | — |
| `deploy/helm/umbrella/values.prod.yaml:2038` | `url: "https://kaname-internal.kacho.svc:9092/iam/v1/hooks/token"` | — |
| `deploy/identity_chart_default_premise_test.go:111-118` | `func TestIdentityDevFlags_ChartDefaultsAreStillSecure(t *testing.T) {` | `vals := chartArchiveValues(t, k.archive)` |
| `deploy/identity_dev_flag_declaration_test.go:78-92` | `func identityDevKnobs() []identityDevKnob {` | `}` |
| `deploy/identity_chart_premise_reachability_test.go:66-97` | `func TestChartPremiseIsActuallyInvoked(t *testing.T) {` | `}` |
| `deploy/identity_chart_premise_reachability_test.go:117-186` | `func TestUmbrellaRenderStillNeedsMaterializedDeps(t *testing.T) {` | `}` |
| `deploy/edge_retired_knobs_render_test.go:4` | `//go:build helmcharts` | — |
| `deploy/identity_chart_premise_reachability_test.go:89-92` | `go test -tags helmcharts ./deploy/...` | `"тег вместе с файлом", ciWorkflow)` |
| `deploy/identity_config_digest_binds_the_same_text_test.go:45` | `var dataFromNamedTemplate = regexp.MustCompile(` | — |
| `deploy/helm/umbrella/charts/kaname/templates/identity-provider-config-configmap.yaml:89` | `{{- include "kacho.identity.configYaml" .` | — |
| `deploy/helm/umbrella/charts/kaname/templates/identity-provider-hooks-configmap.yaml:48` | `{{- include "kacho.identity.hookIdentityPayload" .` | — |
| `deploy/helm/umbrella/charts/kaname/templates/identity-provider-schema-configmap.yaml:28` | `{{- include "kacho.identity.schemaJSON" .` | — |
| `deploy/helm/umbrella/values.prod.yaml:1771-1775` | `value: '{{ printf "%s\n" (include "kacho.identity.configYaml" .)` | `value: '{{ printf "%s\n%s\n" (include "kacho.identity.hookIdentityPayload" .) (include "kacho.identity.hookRecoveryPayload" .)` |
| `deploy/helm/umbrella/charts/kaname/templates/deployment.yaml:89` | `kacho.cloud/config-checksum: {{ include (print $.Template.BasePath "/configmap.yaml") .` | — |
| `deploy/identity_config_digest_binds_the_same_text_test.go:91-95` | `if len(byConfigMap) == 0 {` | `}` |
| `deploy/identity_config_mount_census_test.go:43-51` | `// Ни одно имя тома, карты настроек или профиля здесь не написано. Цепочка вывода:` | `//  4. профили, объявляющие том с ЭТИМ именем и источником-картой, — монтирующие.` |
| `deploy/identity_config_mount_census_test.go:169` | `func identityMountFacts(t *testing.T) mountFacts {` | — |
| `deploy/identity_config_reaches_the_process_test.go:27-30` | `// Карта настроек, чьё СОДЕРЖИМОЕ несёт путь маршрута обратных вызовов, обязана` | `// ей важно, что монтирование ЕСТЬ.` |
| `deploy/identity_config_reaches_the_process_test.go:69` | `const hookRouteMarker = "/iam/v1/hooks/"` | — |
| `deploy/identity_config_reaches_the_process_test.go:111` | `func TestIdentityCallbackConfigIsMountedSomewhere(t *testing.T) {` | — |
| `deploy/identity_config_template_test.go:19-22` | `// identityConfigTemplate — объявление настроек службы личности. Содержимое` | `const identityConfigTemplate = "helm/umbrella/charts/kaname/templates/_identity-provider.tpl"` |
| `deploy/identity_config_template_test.go:24-26` | `// defaultSchemaIDDecl — строка` | `var defaultSchemaIDDecl = regexp.MustCompile(` |
| `deploy/identity_mail_lane_feeds_both_senders_test.go:53-54` | `// mailSenderConfigTemplate — шаблон, рендерящий настройку НАШЕГО отправителя.` | `const mailSenderConfigTemplate = "helm/umbrella/charts/kaname/templates/configmap.yaml"` |
| `deploy/identity_courier_arg_premise_test.go:54` | `const identityProviderArchiveGlob = "` | — |
| `deploy/identity_courier_arg_premise_test.go:117-136` | `func TestCourierArgsAreNotInheritedFromTheDeployment(t *testing.T) {` | `t.Errorf("%s: шаблон почтового процесса СТАЛ читать` |
| `deploy/identity_courier_arg_premise_test.go:184-185` | `serveBody, serveMember := chartArchiveMember(t, archive, "deployment-` | `defaultConfig := regexp.MustCompile(` |
| `deploy/identity_courier_reads_what_it_mounts_test.go:10-18` | `// У службы личности ДВА рабочих объекта: основной процесс (Deployment) и` | `//     и это разные ручки.` |
| `deploy/identity_courier_reads_what_it_mounts_test.go:99` | `const identityProviderKey = "` | — |
| `deploy/identity_courier_reads_what_it_mounts_test.go:178-179` | `c.mainConfigs = configPaths(stringList(dep["extraArgs"]))` | `c.courierConfigs = configPaths(stringList(sts["extraArgs"]))` |
| `deploy/identity_delivery_flows_declared_once_injection_test.go:27` | `copyTree(t, umbrellaDir, root)` | — |
| `deploy/identity_delivery_flows_declared_once_injection_test.go:33` | `return deliveryFlowFindings(t, f.root)` | — |
| `deploy/identity_delivery_flows_declared_once_injection_test.go:54` | `func TestDeliveryFlowGateFailsOnAReturnedDefect(t *testing.T) {` | — |
| `deploy/identity_delivery_flows_declared_once_test.go:65-70` | `var deliveryFlowKey = regexp.MustCompile(` | `var mailLaneKeyInProfile = regexp.MustCompile(` |
| `deploy/identity_delivery_flows_declared_once_test.go:111-122` | `if !identityChainLandsIdentity(t, texts) {` | `mounting++` |
| `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:318-405` | `{{- with .Values.config.authn.login }}` | `{{- end }}` |
| `deploy/identity_delivery_flows_declared_once_test.go:154-159` | `if raising == 0 {` | `}` |
| `deploy/identity_dev_flag_declaration_test.go:244` | `on, ok := lookup(f.effective, k.subchart, "enabled")` | — |
| `deploy/identity_dev_flag_declaration_test.go:268-273` | `if enabled == 0 && flagRead != prodStacks*len(knobs) {` | `}` |
| `deploy/identity_domainless_landing_injection_test.go:12-13` | `// Зовутся ТЕ ЖЕ функции вердикта, что исполняет гейт (` | `), а не их копии: копия` |
| `deploy/identity_domainless_landing_injection_test.go:56` | `value, present := sessionCookieDomain(cfg)` | — |
| `deploy/identity_domainless_landing_injection_test.go:69` | `if v := landingDeclarationVerdict("46.173.29.131", false); v != landingIPNotDeclared {` | — |
| `deploy/identity_domainless_landing_is_expressible_test.go:88-114` | `карты настроек) и разбирает его как YAML.` | `t.Fatalf("в рендере нет карты настроек с ключом` |
| `deploy/identity_domainless_landing_is_expressible_test.go:123-129` | `func sessionCookieDomain(cfg map[string]any) (value string, present bool) {` | `v, ok := cookie["domain"]` |
| `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:331-332` | `{{- with .cookieDomain }}` | `cookie-domain: {{ .` |
| `deploy/identity_domainless_landing_is_expressible_test.go:60` | `const iamSubchartDir = umbrellaDir + "/charts/kaname"` | — |
| `deploy/identity_domainless_landing_is_expressible_test.go:68` | `func renderIdentitySubchart(t *testing.T, valueFiles []string, sets ...string) (string, error) {` | — |
| `deploy/client_token_knobs_of_the_pin_test.go:229` | `rendered, err := renderIdentitySubchart(t, nil, sets...)` | — |
| `deploy/identity_file_keys_survive_the_environment_injection_test.go:12-15` | `// Дефект вносится ровно тем механизмом, которым он приезжает в жизни: ключом` | `// разборщик читает YAML, — а требуется доказать, что гейт видит ПРЕДМЕТ.` |
| `deploy/identity_file_keys_survive_the_environment_injection_test.go:126` | `.config.courier.smtp.connection_uri=smtp://injected.invalid:25/")` | — |
| `deploy/identity_file_keys_survive_the_environment_test.go:102-116` | `// Различает их признак, который производим МЫ: посадка личности, объявленная` | `//	                    поставщика, — находка, а не «своя полоса плюс запас».` |
| `deploy/identity_file_keys_survive_the_environment_test.go:131-135` | `// Поэтому вторая половина судит ту же цепочку, в которой поставщик ПОДНЯТ` | `// пробой, чтобы вердикт о файле настроек не читался как вердикт о стенде.` |
| `internal/repohygiene/retiredidentityvendorceiling.go:6-7` | `// retiredidentityvendorceiling.go — УБЫВАЮЩИЙ ПОТОЛОК привязок к снимаемому` | `// издателю личности (задачи #2730, #2864): число изменения не выше числа его БАЗЫ.` |
| `deploy/identity_flow_path_is_served_injection_test.go:145` | `return flowDecl{file: "_identity-provider.tpl", raw: "{{ $flow }}/registration", vars: identityTemplateVars(prefixKnob)}` | — |
| `deploy/identity_flow_path_is_served_injection_test.go:243` | `file: "_identity-provider.tpl", raw: "{{ $flow }}/login",` | — |
| `deploy/identity_flow_path_is_served_test.go:839` | `if !strings.Contains(body, "ui_url:") {` | — |
| `deploy/identity_flow_path_is_served_test.go:904-906` | `if decls == 0 {` | `"и «ноль находок» здесь означало бы «ноль прочитанного»", helmDeclarationsRoot)` |
| `deploy/identity_flow_path_is_served_test.go:10-15` | `// Об одном предмете говорят ДВА места, и они разошлись. Служба личности` | `// ничем.` |
| `deploy/identity_flow_path_is_served_test.go:88` | `const nginxServingChart = "../ui-future/deploy/templates/configmap-nginx.yaml"` | — |
| `deploy/identity_global_defaults_agree_test.go:71-128` | `func TestIdentityGlobalDefaultsOfTheSubchartAgreeWithTheUmbrella(t *testing.T) {` | `}` |
| `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:640` | `{{- $mailNode := (((.Values.global).kacho).identity).smtp` | — |
| `deploy/identity_global_defaults_agree_test.go:10-13` | `// Авторитетное — в умбрелле (` | `// это свойство Helm, а не наше решение.` |
| `deploy/identity_global_defaults_agree_test.go:134-168` | `const (` | `}` |
| `deploy/identity_hook_provenance_reaches_every_stand_test.go:57` | `const provReportScript = "scripts/identity-hook-credential-provenance.sh"` | — |
| `deploy/scripts/identity-hook-credential-provenance.sh:5-8` | `# identity-hook-credential-provenance.sh — ТРИ ОТПЕЧАТКА ОДНОЙ ВЕЛИЧИНЫ, снятые` | `# прав). Расхождение любых двух — находка.` |
| `deploy/identity_inherited_schemas_are_declared_test.go:30-34` | `// кластера, а не в git. Поэтому проверяется ПРОКСИ, и он назван прямо:` | `//	поставщика; значит наша секция обязана покрывать ЕЁ.` |
| `deploy/identity_inherited_schemas_injection_test.go:47` | `func TestIdentityInheritedSchemasGate_ProvenByInjection(t *testing.T) {` | — |
| `deploy/identity_inherited_schemas_injection_test.go:65` | `got := adjudicateInheritedSchemas("a8f60d", provider, nil, own)` | — |
| `deploy/identity_mail_defaults_are_empty_injection_test.go:26-35` | `func syntheticLayer(smtp map[string]any, hooksScheme string) map[string]any {` | `"smtp":  smtp,` |
| `deploy/identity_mail_defaults_are_empty_injection_test.go:51-56` | `func TestMailDefaultsGateCanStaySilent(t *testing.T) {` | `findings, c := scanMailDefaults(layers)` |
| `deploy/identity_mail_defaults_are_empty_injection_test.go:81` | `func TestMailDefaultsGateCanFail(t *testing.T) {` | — |
| `deploy/identity_mail_defaults_are_empty_test.go:66-72` | `var mailDefaultKnobs = [][]string{` | `}` |
| `deploy/identity_mail_defaults_are_empty_test.go:104-107` | `// mailSmtpNode — узел` | `for _, k := range []string{"global", "kacho", "identity", "smtp"} {` |
| `deploy/identity_mail_defaults_are_empty_test.go:188-215` | `func TestMailDefaultsAreEmpty(t *testing.T) {` | `}` |
| `deploy/identity_mail_defaults_are_empty_test.go:44-45` | `//   - что профиль объявил почтовые величины — это предмет стража рендера (С1) и` | `//     шага подстановки (С2), у каждого своя досягаемость;` |
| `deploy/identity_mail_lane_feeds_both_senders_injection_test.go:24` | `for _, rel := range []string{identityConfigTemplate, mailSenderConfigTemplate} {` | — |
| `deploy/identity_mail_lane_feeds_both_senders_injection_test.go:60-62` | `func TestMAIL48Injection_LawfulTreeIsSilent(t *testing.T) {` | `if got := mailLaneFeedFindings(t, mailFeedCopyTree(t)); len(got) != 0 {` |
| `deploy/identity_mail_lane_feeds_both_senders_injection_test.go:71-79` | `func TestMAIL48Injection_SenderFedFromAnotherNode(t *testing.T) {` | `got := mailLaneFeedFindings(t, root)` |
| `deploy/identity_mail_lane_feeds_both_senders_test.go:6-11` | `// ПРЕДМЕТ. Узел, адрес отправителя и удостоверение объявляются ОДНАЖДЫ` | `// ТОТ ЖЕ.` |
| `deploy/identity_mail_lane_feeds_both_senders_test.go:277-278` | `courier := mailFeedPathsOf(t, root, identityConfigTemplate, mailCourierSectionRe)` | `sender := mailFeedPathsOf(t, root, mailSenderConfigTemplate, mailSenderSectionRe)` |
| `deploy/identity_mail_lane_feeds_both_senders_test.go:314` | `if courier.Feed() != sender.Feed() {` | — |
| `deploy/identity_mail_lane_single_declaration_injection_test.go:29` | `func copyTree(t *testing.T, src, dst string) {` | — |
| `deploy/identity_mail_lane_single_declaration_injection_test.go:65` | `tpl:    filepath.Join(root, "charts", "kaname", "templates", "_identity-provider.tpl"),` | — |
| `deploy/identity_mail_lane_single_declaration_injection_test.go:105` | `func TestMailLaneGateFailsOnAReturnedDefect(t *testing.T) {` | — |
| `deploy/identity_mail_lane_single_declaration_test.go:56` | `const mailLaneFeedPath = "global.kacho.identity.smtp"` | — |
| `deploy/identity_mail_lane_single_declaration_test.go:75-80` | `var mailLaneMention = regexp.MustCompile(` | `var courierSectionDecl = regexp.MustCompile(` |
| `deploy/identity_mail_lane_single_declaration_test.go:237-253` | `if courierSectionDecl.MatchString(body) && f != identityConfigTemplate {` | `в значениях: он замещает наше "+` |
| `deploy/identity_method_comment_matches_declaration_injection_test.go:29` | `body := readFileForTest(t, identityConfigTemplate)` | — |
| `deploy/identity_method_comment_matches_declaration_injection_test.go:37-39` | `func TestIdentityMethodCommentInjection_ScanSeesTheRealDeclaration(t *testing.T) {` | `states, claims, comments := scanIdentityMethodsBlock(identityBodyForInjection(t))` |
| `deploy/identity_method_comment_matches_declaration_test.go:20-25` | `// # Почему гейт читает РАЗОБРАННОЕ объявление` | `// подсудимый, а не источник.` |
| `deploy/identity_method_comment_matches_declaration_test.go:230-243` | `func TestIdentity_MethodStateCommentMatchesDeclaration(t *testing.T) {` | `identityConfigTemplate)` |
| `deploy/helm/umbrella/charts/kaname/templates/_identity-provider.tpl:350-351` | `#   ВЫКЛЮЧЕНЫ: link, oidc` | `#   ВКЛЮЧЕНЫ: code, profile` |
| `deploy/identity_registration_lanes_issue_a_session_test.go:44-72` | `func registrationLaneHooks(t *testing.T) map[string][]string {` | `if strings.HasPrefix(p, "registration:") {` |
| `deploy/identity_registration_lanes_issue_a_session_test.go:103-104` | `func TestIdentity_EveryRegistrationLaneIssuesASession(t *testing.T) {` | `lanes := registrationLaneHooks(t)` |
| `deploy/helm/umbrella/charts/kaname/templates/configmap.yaml:419-425` | `registration:` | `{{- end }}` |
| `deploy/identity_replaced_lists_are_decided_test.go:10-15` | `// Процесс службы личности получает ДВА файла настроек и сливает их по порядку;` | `// обе секции по отдельности валидны.` |
| `deploy/identity_replaced_lists_are_decided_test.go:94-102` | `//	П1. Порядок источников. Подчарт поставщика передаёт процессу` | `//	    читать ту сторону, которую сверяет, и позеленеет на вернувшемся дефекте.` |
| `deploy/identity_replaced_lists_are_decided_test.go:698` | `provNode, ok := lookup(merged, "` | — |
| `deploy/identity_replaced_lists_injection_test.go:9-10` | `// Зовутся ТЕ ЖЕ функции, что исполняет гейт (adjudicateReplacedIdentityLists,` | `// identityProviderLists, identityOurLists), а не их копии: копия предиката` |
| `deploy/identity_replaced_lists_injection_test.go:34` | `func TestReplacedListsGate_VerdictProvenByInjection(t *testing.T) {` | — |
| `deploy/identity_schema_body_single_node_injection_test.go:54-77` | `:\n  identitySchemas:\n    \"identity.default.schema.json\":` | `.identitySchemas",` |
| `deploy/identity_schema_body_single_node_test.go:11-16` | `// Тело схемы личности нужно ДВУМ сторонам настроек сразу: нашей секции (через` | `// строкам, решит порядок слияния, а не решение.` |
| `deploy/identity_schema_body_single_node_test.go:265` | `func TestIdentitySchemaBodyIsDeclaredByASingleNode(t *testing.T) {` | — |
| `deploy/helm/umbrella/charts/kaname/templates/_identity-provider.tpl:198` | `{{- $inherited := $id.inheritedSchemas` | — |
| `deploy/identity_second_factor_reachable_injection_test.go:226` | `func TestIdentitySecondFactorInjection_OwnConsoleDeclarationDecidesTheFloor(t *testing.T) {` | — |
| `deploy/identity_second_factor_reachable_injection_test.go:287` | `func TestIdentitySecondFactorInjection_OwnServiceSideIsReadFromThePinnedRoot(t *testing.T) {` | — |
| `deploy/identity_second_factor_reachable_injection_test.go:365` | `func TestIdentitySecondFactorInjection_ExternalLandingIsARefusal(t *testing.T) {` | — |
| `deploy/identity_second_factor_reachable_injection_test.go:61-63` | `:\n  enabled: true\n  deployment: {}\n",` | `:\n  enabled: false\n",` |
| `deploy/identity_second_factor_reachable_injection_test.go:126-138` | `const foreignOn = "` | `for _, chart := range []string{"` |
| `deploy/identity_second_factor_reachable_injection_test.go:29-37` | `func TestIdentitySecondFactorInjection_ChainPredicatesReadBothSides(t *testing.T) {` | `}` |
| `deploy/identity_second_factor_reachable_test.go:26-41` | `// # Стороны 1 и 2 — СВОИ У КАЖДОЙ ПОСАДКИ (#2691)` | `// ровно там, где пол «2» поднять было нечем. Теперь стороны берутся у посадки.` |
| `deploy/identity_second_factor_reachable_test.go:2242-2256` | `func sidesOfLanding(t *testing.T, landing, console string) (secondFactorSides, error) {` | `return ownSecondFactorSides(pin, root, vocab, rule, console)` |
| `deploy/identity_second_factor_reachable_test.go:2292` | `func TestIdentity_SecondFactorReachesTheBrowser(t *testing.T) {` | — |
| `deploy/identity_second_factor_reachable_test.go:171` | `identityRenderedConfigPath = "/etc/kaname-identity-rendered/` | — |
| `deploy/identity_second_factor_reachable_test.go:2381-2388` | `func identityChainMountsOurConfig(texts []string) bool {` | `}` |
| `deploy/identity_session_secret_source_injection_test.go:28-43` | `func lane(dsn string, secrets map[string]any, secret map[string]any) map[string]any {` | `return map[string]any{"` |
| `deploy/identity_session_secret_source_test.go:10-16` | `.config.secrets.{default,cookie,cipher}` | `// сессии арендаторов.` |
| `deploy/identity_session_secret_source_test.go:191-208` | `, _ := values["` | `inner, _ :=` |
| `deploy/identity_step_declaration_parses_injection_test.go:83` | `func TestStepDeclarationGateFailsOnAReturnedDefect(t *testing.T) {` | — |
| `deploy/identity_step_declaration_parses_injection_test.go:109` | `if !strings.Contains(strings.Join(got, "\n"), "_identity-provider.tpl") {` | — |
| `deploy/identity_step_declaration_parses_test.go:116-119` | `for name, blk := range defineBodies(string(b)) {` | `}` |
| `deploy/helm/umbrella/charts/kaname/templates/_identity-provider.tpl:782` | `{{- define "kacho.identity.configRenderInitContainer" -}}` | — |
| `deploy/identity_step_declaration_parses_test.go:190-192` | `if examined == 0 {` | `"беспредметен, а не зелёный")` |
| `deploy/identity_step_survives_kubernetes_expansion_injection_test.go:49` | `func TestStepExpansionGateFailsOnAReturnedDefect(t *testing.T) {` | — |
| `deploy/identity_step_survives_kubernetes_expansion_injection_test.go:69` | `if !strings.Contains(joined, "_identity-provider.tpl") {` | — |
| `deploy/identity_step_survives_kubernetes_expansion_test.go:52-54` | `//   - популяция — та же, что у соседа identity_step_declaration_parses_test.go` | `//     кодом: две популяции об одном предмете разошлись бы молча;` |
| `deploy/identity_step_survives_kubernetes_expansion_test.go:15-24` | `// Наблюдалось (задача #1786, ревизия bb11485ec, оба рабочих объекта личности,` | `// поднялся, семь проверок линии были красны.` |
| `deploy/identity_step_survives_kubernetes_expansion_test.go:33` | `// скрипт на той же карте настроек в том же образе` | — |
| `deploy/identity_step_survives_kubernetes_expansion_test.go:208` | `func TestIdentityStepSurvivesKubernetesExpansion(t *testing.T) {` | — |
| `deploy/identity_substitution_judges_the_form_test.go:10-14` | `// Шаг, подставляющий величины в конфигурацию службы личности, проверял остаток` | `// потому что искал не её.` |
| `deploy/identity_substitution_judges_the_form_test.go:203-204` | `mf := identityMountFacts(t)` | `f.file, f.body = mf.stepCoord, mf.stepBody` |
| `deploy/identity_substitution_judges_the_form_test.go:216-218` | `// Конфигурация — то объявление, чьё тело несёт маршрут обратных вызовов.` | `if !strings.Contains(blk, callbackRoute) {` |
| `deploy/identity_verification_mirrors_the_requirement_injection_test.go:46` | `return verificationMirrorFindings(t, f.root)` | — |
| `deploy/identity_verification_mirrors_the_requirement_injection_test.go:90` | `func TestVerificationMirrorGateFailsOnAReturnedDefect(t *testing.T) {` | — |
| `deploy/identity_verification_mirrors_the_requirement_test.go:9-12` | `// Вход требует подтверждённого адреса. Подтверждение доставляется письмом, и` | `// ни одна из двух половин по отдельности неверной не выглядит.` |
| `deploy/identity_verification_mirrors_the_requirement_test.go:70` | `var identityConfigInUmbrella = strings.TrimPrefix(identityConfigTemplate, umbrellaDir+"/")` | — |
| `deploy/identity_verification_mirrors_the_requirement_test.go:291-293` | `if len(enablingSources)+len(disablingSources) == 0 {` | `"распознаватель перестал их узнавать либо форма записи сменилась. "+` |
| `deploy/identity_verification_mirrors_the_requirement_test.go:356` | `func TestIdentity_VerificationFlowMirrorsTheVerifiedAddressRequirement(t *testing.T) {` | — |
| `deploy/identity_verified_address_required_on_both_lanes_injection_test.go:23-26` | `const requirementLine = "            - hook: require_verified_address\n"` | `body := readFileForTest(t, identityConfigTemplate)` |
| `deploy/identity_verified_address_required_on_both_lanes_test.go:54` | `const verifiedAddressHook = "require_verified_address"` | — |
| `deploy/identity_verified_address_required_on_both_lanes_test.go:169-170` | `func TestIdentity_VerifiedAddressRequiredOnEveryLoginLane(t *testing.T) {` | `body := readFileForTest(t, identityConfigTemplate)` |
| `deploy/helm/umbrella/charts/kaname/templates/_identity-provider.tpl:446` | `- hook: require_verified_address` | — |
| `deploy/helm/umbrella/charts/kaname/templates/_identity-provider.tpl:461` | `- hook: require_verified_address` | — |
| `deploy/rollout_preflight_covers_required_secrets_test.go:243` | `s := scalarLine.FindStringSubmatch(cur)` | — |
| `deploy/kaname_listener_knobs_test.go:132` | `if !contains(f.surfaces, s) {` | — |
| `deploy/own_ceilings_and_access_keys_umbrella_test.go:269` | `if !contains(pinned, accessKeysCeilingKey) {` | — |
| `deploy/trust_anchor_claim_matches_declaration_test.go:245` | `if contains(l.Markers, "ТОЛЬКО-ВНУТРЕННЕЕ") &&` | — |
| `deploy/db_footprint_declaration_test.go:206` | `prod, why := declaresProduction(declared)` | — |
| `deploy/edge_retired_knobs_render_test.go:57-66` | `for _, d := range decodeRender(t, rendered) {` | `for _, e := range slice(cm, "env") {` |
| `deploy/stand_verdict_carries_provenance_test.go:130` | `var wf provWorkflow` | — |
| `deploy/stand_verdict_carries_provenance_test.go:147` | `for _, recipe := range provStandRecipes {` | — |
| `deploy/own_minting_census_test.go:556` | `merged, files, _ := mergedValuesOfStack(t, stacks[name])` | — |
| `deploy/own_posture_foreign_identity_test.go:365` | `merged, _, _ := mergedValuesOfStack(t, stacks[name])` | — |
