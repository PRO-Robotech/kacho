# kacho-nlb — 09-fga-model

Разбор двух решений модели прав, принятых этим сервисом. Связное описание модели — на сайте
документации; здесь только то, что адресовано инженеру сервиса.

## Mirror-feed на label-Update (by-design, sub-phase T3.1 / #113)

Consumer-обязанность: каждый label-selectable nlb-ресурс эмитит
`InternalIAMService.RegisterResource` (mirror.upsert) с **актуальными labels +
parent_project_id** не только на Create, но и на label-меняющем Update — иначе
IAM `resource_mirror` протухает и ARM_LABELS-грант (γ `matchLabels`-селектор) не
ревокается при снятии/смене метки (root-cause #113).

- **listener — двойной баг (исправлен T3.1):** `listenerRegisterIntent`
  (`listener/create.go`) ранее эмитил bare-intent без `Labels` → селектор не
  матчил даже свежесозданный listener. Теперь Create-intent несёт
  `Labels: domain.LabelsToMap(...)` + `ParentProjectID` (parity с
  `lbMirrorIntent`/`tgMirrorIntent`). Update получил `listenerLabelsInMask`-gated
  emit `listenerMirrorIntent` (parent-link tuple + текущие labels) в writer-tx.
- **G-2 (gate):** эмит только когда labels в маске (empty mask = full-PATCH ⇒
  true). Non-label Update (rename/desc) → no-op (меньше reconcile-шума;
  external-поведение идентично always-emit за счёт `source_version`-monotonic).
- **G-3 (upsert, НЕ Unregister):** полное снятие меток (`labels={}`) эмитит
  `RegisterResource` (mirror.upsert) с пустым labels-map — НЕ `UnregisterResource`.
  Ресурс жив, mirror-строка остаётся; пустые labels корректно протухают
  label-селекторы, не снося owner-tuple/containment. `UnregisterResource` —
  только на Delete ресурса.
- **G-4 (atomicity, SEC-D):** mirror-intent пишется в той же writer-tx, что и
  UPDATE listener'а (один `w.Commit`); rollback Update ⇒ intent не записан (нет
  dual-write). Дрейн в IAM — отдельный at-least-once register-drainer.
- **LB / TargetGroup** уже корректны (эталон `labelsInMask`/`labelsInMaskTG`) —
  T3.1 их код не трогал (non-regression).
- **Ребро не новое:** `nlb→iam RegisterResource` существует (SEC-A owner-tuple);
  T3.1 увеличивает частоту эмита (Update-trigger), не меняет payload-форму и не
  вводит iam→nlb обратного вызова (циклов нет).

## Управление составом группы целей — собственные отношения (NLB-TGT-1)

`AddTargets` / `RemoveTargets` гейтятся **своими** отношениями типа
`nlb_target_group` — `v_addtargets` / `v_removetargets`, — а не отношением правки
самой группы.

**Предмет.** Пока оба RPC требовали `v_update`, право управлять составом было
невыдаваемо отдельно от права менять саму группу: платформенная роль
`loadbalancer.target_manager` объявляет `addTargets`/`removeTargets`, показывает их
владельцу роли как то, что она даёт, — и не давала ничего, потому что отношения с
таким именем у типа не существовало, а глагол вне набора типа реконсайлер
пропускает молча (fail-closed).

**Форма решения.** Оба отношения объявлены **надмножеством** `v_update`:

```
define v_addtargets:    [user, service_account, group#member] or v_update
define v_removetargets: [user, service_account, group#member] or v_update
```

Поэтому у субъекта, который вправе править группу, новый вопрос разрешается
ветвью `or v_update` и находит **тот же** прямой кортеж: переключение гейтинга не
потребовало ре-материализации ни одной выдачи и не имело окна, в котором прежний
держатель отказан. Надмножество **одностороннее** — держатель управления составом
`v_update` и `v_delete` не получает.

**Что осталось на прежнем отношении:** `Update`, `Move`, `Delete` группы. Перенос
между проектами — смена владения, а не управление составом.

**Имена не выбраны, а выведены.** Реконсайлер kaname собирает имя отношения из
авторского глагола правила роли, поэтому `addTargets` даёт ровно `v_addtargets`.
Имя, написанное иначе, адресовало бы отношение, которого у типа нет, и запись была
бы отвергнута владельцем модели окончательно.

**Первый тип платформы с набором шире канонического CRUD.** Набор глаголов — атрибут
типа (`authzmap.VerbRelationsOfType`), а не платформенная константа; гейт дрейфа
сверяет набор с канонической моделью **потипово**. Словарь, **общий для всех**
ресурсов (публичное поле каталога прав `closedVerbs`), — это **пересечение** наборов,
поэтому он остался прежним: два новых глагола на публичную платформенную поверхность
не поехали.
