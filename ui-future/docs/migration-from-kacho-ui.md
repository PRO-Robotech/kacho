# План переноса наработок kacho-ui → kacho-ui-future

Переносим: **NLB** (LoadBalancer / Listener / TargetGroup), **VPC placement**
(ZONAL/REGIONAL для Subnet/LB), и **фиксы** (dropdown-фильтры VIP-source, 401/DPoP+step-up,
Address.used_by, single-overview / кастомные detail). Шаблон для копирования —
существующий `vpc`-remote. Стиль — `CLAUDE.md` (нормативный style-рул).

---

## Статус: что УЖЕ есть в kacho-ui-future (не дублировать)

- **MF-инфраструктура** host + remotes vpc/iam/dashboard, shared-singleton, lazy+Suspense,
  HostContext-prop, navigation-контракт, dev-federation.ps1, nginx-proxy, K8s-deploy.
- **Рукописный api-слой** (`vpc/src/api/*`) с case-конверсией, `ApiError`, DPoP
  (`lib/dpop.ts`), 401/step-up обработка (`lib/api-client.ts`), Kratos (`lib/kratos.ts`),
  context-store, WhoAmI-bootstrap.
- **resource-registry-движок**: FormField-union, RefField/ArrayField/CustomField,
  sanitize/hydrate, `GlobalResourceFormModal`/`ResourceFormModal`/`InlineResourceForm`,
  `ResourceListPage`/`ResourceDetailPage`/`ResourceCreatePage`, FormShell/ResourceFormBody.
- **NLB частично в vpc-REGISTRY**: `load-balancers`, `listeners`, `target-groups` уже
  присутствуют как spec-записи (`vpc/src/lib/resource-registry.tsx`), App.tsx имеет
  `NLB_SCOPED` routes и `TargetGroupDetailPage` (custom). ⚠️ Проверить полноту перед
  работой — возможно, нужен только доперенос custom-detail LB и фиксов.
- **Тема/стилизация** (cva/cn, CSS-vars, AntD-токены), jest+RTL, eslint/prettier/stylelint,
  verification-гейт.

**Вывод:** значительная часть NLB-registry уже портирована в vpc-remote. Остаётся решить
**архитектурный вопрос (Фаза 0)** и доперенести custom-организмы/detail + placement + фиксы.

---

## Фаза 0 — Решение: отдельный nlb-remote или NLB внутри vpc-remote (0.5 дн)

NLB-spec уже живёт в `vpc/resource-registry.tsx` + `App.tsx NLB_SCOPED`. Два пути:
- **(A) NLB как отдельный remote** — чисто по доменам (kacho-nlb — отдельный сервис).
  Требует scaffold (чек-лист H1 в CLAUDE.md). Больше работы, правильнее архитектурно.
- **(B) NLB в составе vpc-remote** — как сейчас, минимум работы, но смешивает домены.

Рекомендация: **(A)** для соответствия polyrepo-доменам (nlb — самостоятельный сервис),
если владелец не требует иного. Ниже план исходит из (A); при (B) фазы 1 (scaffold)
пропускаются, работа идёт в vpc-remote.

---

## Фаза 1 — Scaffold nlb-remote (по чек-листу H1) (1 дн)

Шаблон — `vpc/`. Скопировать и адаптировать:
- `nlb/vite.config.ts` ← `vpc/vite.config.ts` (name `nlb`, exposes `./NlbPage`+
  `./navigation`, `KACHO_PUBLIC_BASE=/nlb-remote/`, proxy-правила идентичны).
- `nlb/package.json`, `jest.config.cjs`, `eslint.config.js`, `.prettierrc`,
  `.stylelintrc.json`, `tsconfig*.json`, `tailwind.config.js`, `postcss.config.js`,
  `src/test/setup.ts` (AntD-мок как `vpc/src/test/setup.ts`).
- host-wiring: `host/vite.config.ts` (+`KACHO_NLB_REMOTE`, fallback :4178),
  `host/src/remotes/nlb.d.ts` + `NlbRemote.tsx` (копия `VpcRemote.tsx`), rail-регистрация.
- `dev-federation.ps1` (+nlb watch→preview :4178), корневой `package.json` (`--prefix nlb`),
  Dockerfile + `deploy/templates` (configmap-nginx `location ^~ /nlb-remote/`,
  deployment-host env `KACHO_UI_NLB_UPSTREAM`, service ui-nlb).
- newman/e2e build-матрицы (`newman-e2e.yml`): build+kind-load `ui-nlb:dev`, mtls off.

**Гейт:** `npm run build --prefix nlb` зелёный, host lazy-грузит пустой NlbPage.

---

## Фаза 2 — api-слой nlb (0.5 дн)

Аналог `vpc/src/api/*`. Перенести из `kacho-ui` доменные хелперы NLB, переиспользуя
generic client/DPoP/case (НЕ новый fetch-слой):
- `nlb/src/api/{client,types,auth,resources,iam,cluster}.ts` (client/auth/iam = копии vpc).
- `resources.ts` — LB/Listener/TargetGroup list/get/create/update/delete + `:attachTargetGroup`/
  `:detachTargetGroup` actions. `types.ts` — request/response, `placement_type`,
  `disabled_announce_zones`, VIP-address-spec.

**Гейт:** typecheck зелёный, ручки бьют реальный gateway (proxy-правила из Фазы 1).

---

## Фаза 3 — REGISTRY nlb (0.5 дн; или ревизия существующей в vpc)

Если (A): перенести `load-balancers`/`listeners`/`target-groups` из vpc-REGISTRY (или из
`kacho-ui/resource-registry.tsx`) в `nlb/src/lib/resource-registry.tsx`. Ключевое:
- **placement_type** — `immutable:true` (в Edit → `ImmutableField`).
- **disabled_announce_zones** — `visibleWhen:{field:'placement_type',equals:'REGIONAL'}`
  (multi-select зон; DRAIN-list, не active).
- VIP — CustomField `NlbAddressSpecField` (Фаза 4), не generic RefField.
- `related` — LB→listeners, LB→target-groups.

**Гейт:** generic List/Create/Edit работают для LB/Listener/TargetGroup.

---

## Фаза 4 — Организмы/формы (custom-виджеты) (1.5 дн)

Портировать из `kacho-ui` кастомные формовые компоненты (adapt под atomic-структуру
`organisms/form/`):
- **NlbAddressSpecField** (`kacho-ui/src/components/form/NlbAddressSpecField.tsx`) —
  per-family (v4/v6) picker: режим subnet/address/public + фильтры `buildVipSource`,
  `subnetPlacementMatches` (совпадение `placement_type`!), `linkAddressFilter`
  (type+family). ⚠️ Фильтр Address по `internal_ipv4_address.subnet_id`, не по address-type.
- **RoutesEditor / SgRulesEditor / SubnetCidrChips** — если ещё нет в nlb (переиспользовать
  из vpc через shared-паттерн; в (A) — скопировать нужные).
- Регистрация NlbAddressSpecField как `CustomField.render` в REGISTRY (Фаза 3).
- **Client-валидация** «хотя бы одно семейство» через `ResourceSpec.validate` (наш фикс).

**Гейт:** VIP-picker фильтрует источники строго по placement; тест happy+negative.

---

## Фаза 5 — Detail-страницы (custom) + фиксы (1.5 дн)

- **LoadBalancerDetailPage** (`kacho-ui/src/pages/LoadBalancerDetailPage.tsx`) —
  кастомная (не generic): `ResourceDetailPage` + `extraTabs`/`secondaryActions`.
  `LbTargetGroupsTab` = RefSelect выбор + inline delete-строка;
  **asymmetric payload**: `buildAttachPayload → {attached_target_group:{target_group_id}}`
  vs `buildDetachPayload → {target_group_id}` — раздельные builder'ы (не generic sanitize).
  Единый overview (`hideOverviewTitle`+`hideStatusRow`), inline-edit, табы-списки
  (registry-driven, filtered), Start/Stop в ops, операции-роут, create-listener CTA.
- **NlbVipCell** (`kacho-ui/src/components/NlbVipCell.tsx`) — обёртка `AddressRefTags`,
  показывает v4+v6 в list-колонке, резолв имя+IP; projectId неявно из `useParams`
  (без projectOverride — фикс).
- **Единый стиль дропдаунов** — в kacho-ui-future уже AntD Select везде (наш U17-фикс уже
  соответствует паттерну D10). **401→login** — уже покрыт `lib/api-client.ts` (C8), богаче
  нашего (DPoP-nonce+step-up).

**Гейт:** attach/detach TG inline работает, VIP-cell резолвит, mirror-поведение сохранено.

---

## Фаза 6 — VPC placement (Subnet/LB ZONAL vs REGIONAL) (1 дн; в vpc-remote)

Работает в `vpc/`, не в nlb:
- **Subnet placement_type колонка** (`kacho-ui/resource-registry.tsx:874-899`): render
  Tag purple+`region_id` для REGIONAL, blue+`zone_id` для ZONAL. Legacy без значения →
  ZONAL.
- **Subnet REGISTRY**: `placement_type` immutable; `region_id` `visibleWhen`/required
  только при REGIONAL (иначе backend требует `zone_id` при ZONAL).
- **Address.used_by** (`kacho-ui/resource-detail-extensions.tsx`): `renderUsedBy` на
  Address-detail — referrer-теги `{type,id,name}` с navigate (наш usedBy-фикс).
  ⚠️ Только Address (не NIC/SG).

**Гейт:** placement виден в списке, immutable в edit, used_by кликабелен.

---

## Фаза 7 — Тесты (colocated RTL) (1 дн)

Для каждого нового организма/страницы/поля — `Name.test.tsx` (RTL, getByRole/userEvent,
fetch-mock). Особо защитить (mirror + фиксы):
- asymmetric attach/detach payload (правильная wire-форма);
- VIP-фильтр по placement (не показывает чужой placement);
- placement_type immutable в edit;
- used_by рендер только на Address;
- 401→login (не 403).
`--passWithNoTests` для assertion-only страниц.

**Гейт:** `npm run test && lint && build` по всем модулям зелёные.

---

## Фаза 8 — Host-wiring финал + deploy + e2e (0.5 дн)

- Rail/navigation nlb в host (из `nlb/src/navigation.ts`).
- Deploy: ui-nlb service/deployment/nginx-location, host env upstream.
- newman/e2e umbrella-матрицы (build+kind-load ui-nlb:dev).
- Прогон против живого стека (mirror-verification): header без лишних иконок, реальные IAM
  breadcrumb-ручки, LB CRUD + attach/detach + placement.

**Гейт:** e2e зелёный на задеплоенном стеке (unit ≠ «работает»).

---

## Порядок и оценка

| Фаза | Что | Оценка | Зависит от |
|---|---|---|---|
| 0 | Решение remote vs vpc-inline | 0.5д | — |
| 1 | Scaffold nlb-remote | 1д | 0 |
| 2 | api-слой | 0.5д | 1 |
| 3 | REGISTRY | 0.5д | 2 |
| 4 | Организмы/формы (VIP-picker) | 1.5д | 3 |
| 5 | Detail + фиксы (attach/detach, VipCell) | 1.5д | 4 |
| 6 | VPC placement + used_by (vpc-remote) | 1д | параллельно 1-5 |
| 7 | Тесты | 1д | 4-6 |
| 8 | Host-wiring + deploy + e2e | 0.5д | 7 |

**Итого ≈ 8 дней** (при (B) без отдельного remote: −1.5д на Фазе 1). Фаза 6 (vpc
placement) независима от NLB — можно вести параллельно. Критический путь: 0→1→2→3→4→5→7→8.

**Файлы-шаблоны из vpc-remote:** `vite.config.ts`, `jest.config.cjs`, `eslint.config.js`,
`src/test/setup.ts`, `src/api/{client,auth,iam}.ts`, `lib/{dpop,api-client,kratos,
context-store,use-operation,use-resource-list}.ts`, `resource-registry.tsx`,
`components/organisms/{form/*,ResourceListPage,ResourceDetailPage,ResourceCreatePage,
GlobalResourceFormModal,InlineResourceForm}`, `host/src/remotes/VpcRemote.tsx`+`vpc.d.ts`.
