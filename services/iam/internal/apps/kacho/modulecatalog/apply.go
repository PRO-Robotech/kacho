// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package modulecatalog — ПРИМЕНИТЕЛЬ КАТАЛОГА модуля: читает манифест как данные
// и приводит строки `kacho_iam.catalog_module` / `catalog_resource` /
// `catalog_verb` к объявленному состоянию (задача продукта #1034).
//
// ─────────────────────────────────────────────────────────────────────────────
// ЗАЧЕМ ГЛАГОЛ, ЕСЛИ СТРОКИ УЖЕ ЕСТЬ
//
// Сегодня каталог наполняет ПОСЕВ МИГРАЦИИ, а сходство посева с объявлением
// держит СВЕРКА на старте (`seed.AssertCatalogParity`). Сверка отвечает на вопрос
// «разошлись ли они», и это верный вопрос — но у него нет производителя строк:
// новый ресурс модуля по-прежнему требует ПЕРЕСБОРКИ ОБРАЗА iam, потому что
// миграции встроены в образ (`//go:embed *.sql`), а исполняет их initContainer
// того же образа. Обхода нет: `embed` разрешается на этапе сборки.
//
// Применитель и есть недостающий производитель. Со сверкой он не спорит и её не
// заменяет: перевод опорной стороны сверки с литерала на применённые манифесты —
// отдельный предмет (#1861), и до него применитель обязан производить РОВНО то
// множество, которое называет литерал. Что он его производит — утверждает не эта
// строка, а гейт `rows_test.go`: шесть доставляемых манифестов дают 27 ресурсов и
// 135 глаголов, расхождений с литералом ноль в обе стороны.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОРЯДОК ВНУТРИ ТРАНЗАКЦИИ — ЕГО ДЕРЖИТ КЛЮЧ, А НЕ ПАМЯТЬ АВТОРА
//
//  1. глобальный консультативный замок
//  2. чтение живых строк ЭТОГО модуля
//  3. оживление/заведение: модуль → ресурсы → глаголы
//  4. переселение проекций арендаторских ролей, теряющих референт
//  5. снятие глаголов, которых манифест больше не объявляет
//  6. снятие ресурсов, которых манифест больше не объявляет
//  7. приведение третьей проекции правила к каталожному факту
//  8. сверка ОПОРЫ стража паритета — до коммита
//
// Шаги 3 и 5-6 разнесены НЕ ради красоты: ключ живости идёт от глагола к ресурсу
// (`catalog_verb_resource_live_fk`) и от ресурса к модулю
// (`catalog_resource_module_live_fk`), поэтому вверх порядок «модуль → ресурс →
// глагол», а вниз обратный — «глагол → ресурс». Перепутав, получишь `23503` на
// своём же операторе, а не тихо неверное состояние.
//
// Шаг 4 стоит ПЕРЕД 5-6 по той же причине и это ГЛАВНОЕ свойство применителя:
// проекция правила ссылается на живую строку каталога
// (`role_rule_ref_res_fk`, `role_rule_ref_verb_fk`, `role_verb_type_fk`), ключи
// объявлены `DEFERRABLE INITIALLY IMMEDIATE`, и применитель их НЕ откладывает.
// Значит снятие, поставленное раньше переселения, отвергается НА СВОЁМ
// ОПЕРАТОРЕ — порядок держится ключом, и проба это утверждает перестановкой.
//
// Откладывать ключи (`SET CONSTRAINTS … DEFERRED`) применитель не вправе: тогда
// отказ приехал бы на коммите, единственным свидетелем неверного порядка стал бы
// текст коммита, и «применитель написан верно» перестало бы быть проверяемым.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЗАМОК ГЛОБАЛЬНЫЙ, А НЕ ПО МОДУЛЮ
//
// Ключ один на весь каталог, потому что переселение (шаг 4) трогает проекции
// ролей, а роль ЧУЖОГО модуля вправе называть ресурс этого: правило несёт пару
// «модуль.ресурс», и подстановка ресурса законна в пределах модуля-владельца
// роли, но САМА роль общая. Замок по модулю сериализовал бы два применения одного
// модуля и пропустил бы два применения разных — то есть ровно тот случай, ради
// которого он и берётся.
//
// Замок транзакционный (`pg_advisory_xact_lock`): он снимается коммитом и
// откатом, поэтому оборванный применитель не оставляет каталог запертым.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ПРИМЕНИТЕЛЬ НЕ ДЕЛАЕТ — И ЭТО НЕСУЩЕЕ
//
//  1. **не снимает МОДУЛЬ.** Отсутствие файла манифеста снятием НЕ является
//     (#1034 дословно, приёмка `module-withdrawal-is-described.md` §2.9): причина
//     отсутствия строки системе неизвестна — удаление, конфликт слияния, неполный
//     ConfigMap и откат правки выглядят одинаково. Применитель применяет ОДИН
//     названный манифест и о неназванных модулях не утверждает ничего;
//  2. **не удаляет ни одной строки.** Снятие — ПОМЕТКА (`retired_at`, `live =
//     false`), как и у роли модуля
//     (`services/iam/docs/engineering/architecture/role-withdrawal-is-a-mark.md`).
//     Удаление строки каталога унесло бы каскадом проекции правил молча, а
//     обратимость стоит на том, что снятая строка занимает первичный ключ и
//     повторная установка её ОЖИВЛЯЕТ;
//  3. **не переселяет проекции СИСТЕМНЫХ ролей.** Роль системного яруса объявлена
//     манифестом; если манифест снимает ресурс, который его же роль называет, то
//     манифест противоречит сам себе, и это отвергается ключом — а не чинится
//     молчаливым отбором права у роли, которую применитель не объявлял;
//  4. **не заводит третьей популяции сироты.** Закрытый набор
//     (`role_grant_orphan_source_known`) знает две — выдачу и объявление правила,
//     — и третьей у него нет by construction. Селекторы применитель ПРИВОДИТ к
//     каталожному факту (шаг 7), а не переселяет: запись об отобранном праве уже
//     сделана второй проекцией того же правила, а у подстановочного правила
//     отбирать нечего — его массив есть рендер платформенного набора типов.
//     Довод целиком и то, чего это НЕ закрывает, — `prune.go`.
package modulecatalog

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/PRO-Robotech/kacho/services/iam/internal/catalog"
	"github.com/PRO-Robotech/kacho/services/iam/internal/manifest"
)

var (
	// ErrDerive — манифест не даёт строк каталога. Отдельный отказ: он приходит
	// ДО открытия транзакции и чинится правкой манифеста, а не состоянием базы.
	ErrDerive = errors.New("modulecatalog: manifest does not yield catalog rows")
	// ErrWriteFailed — писатель отказал. Несёт дословный отказ оператора: порядок
	// и живость держит БАЗА, и имя нарушенного ограничения — часть разбора.
	ErrWriteFailed = errors.New("modulecatalog: writing the declared catalog failed")
)

// CatalogWriter — то, что применителю нужно от писателя. Порт объявлен ЗДЕСЬ, в
// use-case: он описывает потребность, а не таблицу.
//
// Отказы приходят СЫРЫМИ (`*pgconn.PgError` достижим через errors.As), и это
// решение, а не упущение: применитель административный, его вызывающий —
// оператор установки, а имя нарушенного ограничения и есть то, чем он чинит.
// Приведение к статусу принадлежит транспорту, который у этого глагола появится
// вместе со своим предметом; приведя здесь, мы потеряли бы имя ограничения
// раньше, чем оно кому-нибудь пригодилось.
type CatalogWriter interface {
	// LockCatalog берёт ГЛОБАЛЬНЫЙ транзакционный консультативный замок каталога.
	LockCatalog(ctx context.Context) error
	// ReadModule читает ЖИВЫЕ строки одного модуля ОДНИМ обращением: ресурс уже
	// снят, а его глаголы ещё живы — состояние, которого в базе не бывает ни при
	// каком порядке применения, и собранный из трёх моментов снимок его выдумал бы.
	ReadModule(ctx context.Context, module string) (catalog.Rows, error)
	// ReadCatalog читает ВЕСЬ каталог ОБЕИМИ половинами под той же транзакцией —
	// вход сверки опоры (шаг 8).
	//
	// Весь, а не один модуль: опора есть литерал ПЛАТФОРМЫ целиком, и строки
	// прочих модулей приехали бы сверке недостающими. Обе половины ОДНИМ методом:
	// подавший стражу одно живое множество получит законное снятие недостающей
	// строкой, а `CatalogState` требует обе позиционно — пропустить снятую
	// сторону нечем.
	ReadCatalog(ctx context.Context) (CatalogState, error)
	// UpsertModule заводит либо ОЖИВЛЯЕТ строку модуля. `changed` ложно, когда
	// объявленное состояние уже стоит в строке.
	UpsertModule(ctx context.Context, module string) (bool, error)
	// UpsertResource заводит либо оживляет строку ресурса.
	UpsertResource(ctx context.Context, r catalog.ResourceRow) (bool, error)
	// UpsertVerb заводит либо оживляет строку действия, приводя и признак словаря.
	UpsertVerb(ctx context.Context, v catalog.VerbRow) (bool, error)
	// ResettleTenantProjections переселяет в `role_grant_orphan` проекции
	// АРЕНДАТОРСКИХ ролей, теряющих референт вместе со снимаемыми строками.
	// Системные роли не трогает — см. шапку, п. 3.
	ResettleTenantProjections(ctx context.Context,
		resources []catalog.ResourceRow, verbs []catalog.VerbRow, reason string) (Resettled, error)
	// RetireVerb помечает строку действия снятой.
	RetireVerb(ctx context.Context, v catalog.VerbRow, reason string) (bool, error)
	// RetireResource помечает строку ресурса снятой.
	RetireResource(ctx context.Context, r catalog.ResourceRow, reason string) (bool, error)
	// PruneRetiredSelectorTypes приводит ТРЕТЬЮ проекцию правила к каталожному
	// факту: вырезает из `role_rule_selectors.object_types` арендаторских ролей
	// элементы, не называющие ЖИВОЙ строки каталога. Трогает только строки,
	// пересекающиеся с названными ресурсами, — предмет вырезания есть снятие,
	// а не таблица целиком.
	PruneRetiredSelectorTypes(ctx context.Context, resources []catalog.ResourceRow) (Pruned, error)
}

// Pruned — сколько вырезано из третьей проекции.
//
// Две величины, а не одна: «тронута одна строка» не говорит, вырезан из неё один
// элемент или пять, а «вырезано пять» не говорит, у одной роли или у пяти. Для
// того, кто разбирает последствия, это разные ответы.
type Pruned struct {
	// Rows — строк селекторов УКОРОЧЕНО (живые типы в них остались).
	Rows int
	// Dropped — строк селекторов СНЯТО целиком: живого типа не осталось ни
	// одного, а пустой массив запрещён ограничением схемы. Отдельная величина, а
	// не часть Rows: укоротить правило и снять его проекцию — события разного
	// рода для того, кто разбирает последствия.
	Dropped int
	// Elements — элементов массива вырезано суммарно, по обеим ветвям.
	Elements int
}

// TxRunner — исполнение под ОДНОЙ транзакцией записи. Все шаги ложатся вместе
// либо не ложатся вовсе: между оживлением и снятием иначе помещается чужое
// чтение, и каталог был бы виден в состоянии, которого манифест не объявлял.
type TxRunner interface {
	RunInWriteTx(ctx context.Context, fn func(ctx context.Context, w CatalogWriter) error) error
}

// Resettled — сколько строк проекции переселено в сироты, по популяциям.
//
// Две величины, а не одна: «право отобрано» (`role_verb`) и «правило перестало
// резолвиться» (`role_rule_ref`) — разные события для того, кто разбирает
// последствия, и сложив их, мы потеряли бы именно это различие.
type Resettled struct {
	RuleRefs  int
	RoleVerbs int
}

// Report — перепись применения. Печатается числами, потому что «применено» без
// чисел неотличимо от «прошло мимо»: применитель, не нашедший ни одной своей
// строки, молчит ровно так же уверенно, как записавший все.
type Report struct {
	Module string
	// DeclaredResources / DeclaredVerbs — сколько объявил манифест.
	DeclaredResources int
	DeclaredVerbs     int
	// WrittenResources / WrittenVerbs — заведено либо оживлено.
	WrittenResources int
	WrittenVerbs     int
	// UnchangedResources / UnchangedVerbs — объявленное уже стояло в строке.
	UnchangedResources int
	UnchangedVerbs     int
	// ModuleWritten — строка модуля заведена либо оживлена.
	ModuleWritten bool
	// RetiredResources / RetiredVerbs — строк помечено снятыми.
	RetiredResources int
	RetiredVerbs     int
	// Resettled — переселённые проекции арендаторских ролей.
	Resettled Resettled
	// PrunedSelectorRows / PrunedSelectorRowsDropped / PrunedSelectorTypes —
	// приведение ТРЕТЬЕЙ проекции к каталожному факту (см. `prune.go`).
	PrunedSelectorRows        int
	PrunedSelectorRowsDropped int
	PrunedSelectorTypes       int
}

// Changed — применение изменило хоть одну строку.
//
// Это и есть ответ на вопрос идемпотентности: второе применение подряд обязано
// вернуть ложь. Не «прошло без ошибки» — прошло бы и притворство.
func (r Report) Changed() bool {
	return r.ModuleWritten ||
		r.WrittenResources > 0 || r.WrittenVerbs > 0 ||
		r.RetiredResources > 0 || r.RetiredVerbs > 0 ||
		r.Resettled.RuleRefs > 0 || r.Resettled.RoleVerbs > 0 ||
		r.PrunedSelectorRows > 0 || r.PrunedSelectorRowsDropped > 0
}

// String — перепись одной строкой.
func (r Report) String() string {
	return fmt.Sprintf(
		"модуль %s · объявлено ресурсов %d глаголов %d · записано %d/%d · без изменений %d/%d · "+
			"снято %d/%d · переселено правил %d выдач %d · селекторов укорочено %d "+
			"снято %d элементов вырезано %d · изменения %t",
		r.Module, r.DeclaredResources, r.DeclaredVerbs,
		r.WrittenResources, r.WrittenVerbs,
		r.UnchangedResources, r.UnchangedVerbs,
		r.RetiredResources, r.RetiredVerbs,
		r.Resettled.RuleRefs, r.Resettled.RoleVerbs,
		r.PrunedSelectorRows, r.PrunedSelectorRowsDropped, r.PrunedSelectorTypes,
		r.Changed())
}

// Applier — применитель. Состояния не держит: повторный прогон — штатный режим.
type Applier struct {
	tx TxRunner
	// obs — наблюдатель применения; nil означает «не наблюдаем» (см. observe.go).
	obs Observer
}

// NewApplier собирает применитель над исполнителем транзакций.
func NewApplier(tx TxRunner) *Applier { return &Applier{tx: tx} }

// Apply приводит строки каталога модуля к объявленному манифестом состоянию.
//
// Причина снятия пишется В СТРОКУ: снятая строка без причины — строка, за
// которую никто не отвечает, а `retired_reason` для того и заведена.
func (a *Applier) Apply(ctx context.Context, m *manifest.Manifest) (Report, error) {
	declared, err := RowsOf(m)
	if err != nil {
		return Report{Module: m.Module}, fmt.Errorf("%w: %w", ErrDerive, err)
	}

	rep := Report{
		Module:            declared.Module,
		DeclaredResources: len(declared.Resources),
		DeclaredVerbs:     len(declared.Verbs),
	}
	reason := "не объявлен манифестом модуля " + declared.Module

	err = a.tx.RunInWriteTx(ctx, func(ctx context.Context, w CatalogWriter) error {
		// Замок — ПЕРВЫМ оператором транзакции. Взятый позже, он оставил бы окно,
		// в котором два применения уже прочитали каталог и оба считают свой снимок
		// действительным.
		if lerr := w.LockCatalog(ctx); lerr != nil {
			return fmt.Errorf("%w: замок каталога: %w", ErrWriteFailed, lerr)
		}
		live, rerr := w.ReadModule(ctx, declared.Module)
		if rerr != nil {
			return fmt.Errorf("%w: чтение каталога модуля: %w", ErrWriteFailed, rerr)
		}

		if changed, uerr := w.UpsertModule(ctx, declared.Module); uerr != nil {
			return fmt.Errorf("%w: модуль %s: %w", ErrWriteFailed, declared.Module, uerr)
		} else if changed {
			rep.ModuleWritten = true
		}
		for _, r := range declared.Resources {
			changed, uerr := w.UpsertResource(ctx, r)
			if uerr != nil {
				return fmt.Errorf("%w: ресурс %s.%s: %w", ErrWriteFailed, r.Module, r.Resource, uerr)
			}
			if changed {
				rep.WrittenResources++
			} else {
				rep.UnchangedResources++
			}
		}
		for _, v := range declared.Verbs {
			changed, uerr := w.UpsertVerb(ctx, v)
			if uerr != nil {
				return fmt.Errorf("%w: действие %s.%s.%s: %w",
					ErrWriteFailed, v.Module, v.Resource, v.Verb, uerr)
			}
			if changed {
				rep.WrittenVerbs++
			} else {
				rep.UnchangedVerbs++
			}
		}

		staleResources, staleVerbs := stale(live, declared)
		if len(staleResources) > 0 || len(staleVerbs) > 0 {
			resettled, serr := w.ResettleTenantProjections(ctx, staleResources, staleVerbs, reason)
			if serr != nil {
				return fmt.Errorf("%w: переселение проекций: %w", ErrWriteFailed, serr)
			}
			rep.Resettled = resettled
		}

		// Вниз порядок обратный: глагол ссылается на живой ресурс, поэтому ресурс
		// снимается ПОСЛЕ своих глаголов.
		for _, v := range staleVerbs {
			retired, terr := w.RetireVerb(ctx, v, reason)
			if terr != nil {
				return fmt.Errorf("%w: снятие действия %s.%s.%s: %w",
					ErrWriteFailed, v.Module, v.Resource, v.Verb, terr)
			}
			if retired {
				rep.RetiredVerbs++
			}
		}
		for _, r := range staleResources {
			retired, terr := w.RetireResource(ctx, r, reason)
			if terr != nil {
				return fmt.Errorf("%w: снятие ресурса %s.%s: %w",
					ErrWriteFailed, r.Module, r.Resource, terr)
			}
			if retired {
				rep.RetiredResources++
			}
		}

		// Шаг 7 — ПОСЛЕ снятия, и это несущее: вырезание оставляет в массиве
		// только элементы, называющие ЖИВУЮ строку каталога. Стой оно раньше,
		// снимаемая строка была бы ещё жива и уцелела бы ровно та, ради которой
		// вырезание и делается. Довод целиком — `prune.go`.
		if len(staleResources) > 0 {
			pruned, perr := w.PruneRetiredSelectorTypes(ctx, staleResources)
			if perr != nil {
				return fmt.Errorf("%w: вырезание снятых типов из селекторов: %w",
					ErrWriteFailed, perr)
			}
			rep.PrunedSelectorRows = pruned.Rows
			rep.PrunedSelectorRowsDropped = pruned.Dropped
			rep.PrunedSelectorTypes = pruned.Elements
		}

		// Шаг 8 — СВЕРКА ОПОРЫ, до коммита. Судится состояние, которое
		// применение ПРОИЗВЕЛО, а не предсказанное: строки читаются той же
		// транзакцией, поэтому вердикт относится к тому, что ляжет.
		state, cerr := w.ReadCatalog(ctx)
		if cerr != nil {
			return fmt.Errorf("%w: чтение каталога для сверки опоры: %w", ErrWriteFailed, cerr)
		}
		plan, aerr := AnchorVerdictOf(ctx, state)
		if aerr != nil {
			return fmt.Errorf("%w: сверка опоры: %w", ErrWriteFailed, aerr)
		}
		if plan.Verdict != VerdictWouldApply {
			return fmt.Errorf("%w: модуль %s: живыми остались бы строки, которых опора не "+
				"знает [%s]; опора называет, а строки не было бы ни живой, ни снятой [%s]. "+
				"Снято решением %d строк — они расхождением НЕ считаются. Пройди такое "+
				"применение, следующий пуск отказал бы страж паритета, и починить это можно "+
				"было бы только прямым SQL — поэтому оно отвергнуто ДО коммита "+
				"(kacho#1034, IAM-MA-1-11)",
				ErrBeyondAnchor, declared.Module,
				strings.Join(plan.BeyondAnchorExtra, ", "),
				strings.Join(plan.BeyondAnchorMissing, ", "),
				len(plan.WithdrawnRows))
		}
		return nil
	})
	// Доклад — ПОСЛЕ закрытия транзакции и на обоих исходах: строки, сосчитанные
	// откаченной транзакцией, в базе не появились, а «отказов ноль» без
	// знаменателя неотличимо от «применений не было вовсе».
	a.observe(rep, err)
	if err != nil {
		return rep, err
	}
	return rep, nil
}

// stale — живые строки модуля, которых манифест больше не объявляет.
//
// Сравнение идёт по ТОМУ ЖЕ ключу, каким строку адресует схема (пара для ресурса,
// тройка для действия). Признак словаря в ключ снятия НЕ входит намеренно: строка
// с тем же именем и другим признаком — та же строка первичного ключа, и она
// приводится оживлением, а не снимается и заводится заново.
func stale(live catalog.Rows, declared Declared) ([]catalog.ResourceRow, []catalog.VerbRow) {
	declaredRes := make(map[string]bool, len(declared.Resources))
	for _, r := range declared.Resources {
		declaredRes[r.Module+"."+r.Resource] = true
	}
	declaredVerb := make(map[string]bool, len(declared.Verbs))
	for _, v := range declared.Verbs {
		declaredVerb[v.Module+"."+v.Resource+"."+v.Verb] = true
	}

	var staleResources []catalog.ResourceRow
	for _, r := range live.Resources {
		if !declaredRes[r.Module+"."+r.Resource] {
			staleResources = append(staleResources, r)
		}
	}
	var staleVerbs []catalog.VerbRow
	for _, v := range live.Verbs {
		if !declaredVerb[v.Module+"."+v.Resource+"."+v.Verb] {
			staleVerbs = append(staleVerbs, v)
		}
	}
	return staleResources, staleVerbs
}
