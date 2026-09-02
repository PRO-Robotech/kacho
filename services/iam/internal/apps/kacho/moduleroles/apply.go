// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package moduleroles — ПРИМЕНИТЕЛЬ ролей модуля: читает манифест как данные и
// приводит строки системных ролей своего модуля к объявленному состоянию
// (приёмка `services/iam/docs/engineering/acceptance/roles-come-as-data-not-migrations.md`,
// §3.1, §3.3, §3.5, §3.7; задача #1824).
//
// # Почему писателем не может быть миграция
//
// Миграции встроены в образ (`//go:embed *.sql` → `migrations.FS`), а исполняет
// их initContainer ТОГО ЖЕ образа. Значит между «модуль объявил роль» и «роль
// есть в базе» стоит пересборка образа iam. Обхода нет: `embed` разрешается на
// этапе сборки. Требование «iam — стандартный образ, модуль привозит своё
// данными» при исполнителе-миграции невыполнимо BY CONSTRUCTION, а не по
// недосмотру.
//
// # Почему писателем не может быть публичный API
//
// `RoleService.Create` системную роль произвести не может: путь пользовательской
// роли не пишет `cluster_id`, а `is_system` вычисляется ровно из него. Это не
// дефект, а решение — иначе арендатор получил бы запись в кластерный ярус.
//
// # Что применитель НЕ делает — и это несущее
//
//  1. **не удаляет ничего.** Роль с выдачами удалить нельзя (`ON DELETE
//     RESTRICT`), а если бы было можно, каскад унёс бы селекторы, проекцию
//     глаголов и проекцию сегментов МОЛЧА. Отзыв роли — предмет #1823;
//  2. **не приводит имя к другому написанию.** Имя роли — аргумент хеша,
//     дающего `id`; «нормализация» дала бы другой `id` и разорвала бы выдачи
//     (§3.7). Имя берётся ДОСЛОВНО;
//  3. **не досевает две проекции из трёх.** Глаголы и селекторы самолечатся на
//     старте, и второй их писатель запрещён (#1028). ТРЕТЬЮ — проекцию
//     объявленных сегментов правила — применитель пишет сам и в той же
//     транзакции: досева у неё нет ни одной строкой, и без записи правило
//     пережило бы свой референт;
//  4. **не трогает роли без модуля-владельца** (`admin`, `edit`, `view`,
//     `owner`, `kacho-system.*`): их первый сегмент не член закрытого набора
//     модулей платформы, поэтому манифестом они невыразимы by construction.
package moduleroles

import (
	"context"
	"errors"
	"fmt"

	"github.com/PRO-Robotech/kacho/services/iam/internal/domain"
	"github.com/PRO-Robotech/kacho/services/iam/internal/manifest"
)

var (
	// ErrRoleRejectedByDomain — объявленная роль не проходит проверку домена.
	// Отдельный отказ: он приходит ДО писателя, и чинится он правкой манифеста,
	// а не состоянием базы.
	ErrRoleRejectedByDomain = errors.New("moduleroles: declared role is rejected by the domain")
	// ErrRolePolicyNotCompilable — правило роли не сворачивается в разрешения.
	ErrRolePolicyNotCompilable = errors.New("moduleroles: declared role policy does not compile")
	// ErrWriteFailed — писатель отказал. Несёт дословный отказ оператора:
	// инварианты держит БАЗА, и её текст — часть контракта.
	ErrWriteFailed = errors.New("moduleroles: writing the declared role failed")
)

// RoleWriter — то, что применителю нужно от писателя. Порт объявлен ЗДЕСЬ, в
// use-case: он описывает потребность, а не таблицу.
type RoleWriter interface {
	// UpsertSystemRole заводит либо приводит строку системной роли. `changed`
	// ложно, когда объявленное состояние уже стоит в строке.
	UpsertSystemRole(ctx context.Context, r domain.Role) (domain.Role, bool, error)
	// ReplaceRuleRefs заменяет проекцию ОБЪЯВЛЕННЫХ сегментов правила ПОЛНОСТЬЮ:
	// сегмент, снятый из правил, обязан исчезнуть и отсюда.
	ReplaceRuleRefs(ctx context.Context, roleID domain.RoleID, refs []domain.RoleRuleRef) error
}

// TxRunner — исполнение под ОДНОЙ транзакцией записи. Строка роли и её проекция
// сегментов пишутся вместе: между ними иначе помещается снятие строки каталога,
// и правило переживёт свой референт (запрет #10).
type TxRunner interface {
	RunInWriteTx(ctx context.Context, fn func(ctx context.Context, w RoleWriter) error) error
}

// Applier — применитель. Состояния не держит: повторный прогон — штатный режим.
type Applier struct {
	tx TxRunner
}

// NewApplier собирает применитель над исполнителем транзакций.
func NewApplier(tx TxRunner) *Applier { return &Applier{tx: tx} }

// Report — перепись применения. Печатается числами, потому что «применено»
// без чисел неотличимо от «прошло мимо»: применитель, не нашедший ни одной
// своей роли, молчит ровно так же уверенно, как записавший все.
type Report struct {
	// Module — модуль манифеста.
	Module string
	// Declared — ролей ЭТОГО применителя: кластерного яруса и своего модуля.
	Declared int
	// Written — строк заведено либо приведено.
	Written int
	// Unchanged — объявленное состояние уже стояло в строке.
	Unchanged int
	// Skipped — ролей манифеста, которые применитель не пишет: иной ярус.
	// Считается отдельно, иначе «ноль записей» читалось бы как «нечего писать».
	Skipped int
	// Names — имена записанных ролей в порядке объявления.
	Names []string
}

// String — перепись одной строкой.
func (r Report) String() string {
	return fmt.Sprintf("модуль %s · объявлено кластерных %d · записано %d · без изменений %d · "+
		"иного яруса %d", r.Module, r.Declared, r.Written, r.Unchanged, r.Skipped)
}

// Apply приводит строки системных ролей модуля к объявленному манифестом
// состоянию.
//
// # Что здесь ОБЪЯВЛЕННОЕ, а что ВЫВЕДЕННОЕ
//
// Объявлены имя (`roles[].id` дословно), назначение и правила. Выведены:
// `id` — функцией имени (`domain.SystemRoleID`), якорь — синглтоном кластера,
// разрешения — сворачиванием правил. Ничего из выведенного манифест не несёт, и
// принимать его оттуда было бы вторым объявлением одного предмета.
func (a *Applier) Apply(ctx context.Context, m *manifest.Manifest) (Report, error) {
	rep := Report{Module: m.Module}

	declared := make([]domain.Role, 0, len(m.Roles))
	for i := range m.Roles {
		mr := &m.Roles[i]
		if mr.Tier == nil || mr.Tier.TierType != domain.ScopeTypeClusterDotted {
			// Ярус аккаунта и проекта уезжает своим путём — `RoleService.Create`.
			// Молча посчитать его «применённым» значило бы обещать запись,
			// которой этот писатель не делает.
			rep.Skipped++
			continue
		}
		r, err := roleOf(mr)
		if err != nil {
			return rep, err
		}
		declared = append(declared, r)
	}
	rep.Declared = len(declared)
	if rep.Declared == 0 {
		return rep, nil
	}

	err := a.tx.RunInWriteTx(ctx, func(ctx context.Context, w RoleWriter) error {
		for _, r := range declared {
			out, changed, uerr := w.UpsertSystemRole(ctx, r)
			if uerr != nil {
				return fmt.Errorf("%w: %s: %w", ErrWriteFailed, r.Name, uerr)
			}
			if !changed {
				rep.Unchanged++
				continue
			}
			// Проекция ОБЪЯВЛЕННЫХ сегментов пишется ПОЛНОЙ заменой и в этой же
			// транзакции: ключи в каталог стоят на ней, а не на колонке `rules`,
			// поэтому приведение, тронувшее правила и не тронувшее проекцию,
			// оставило бы их несогласованными молча. Отказ ключа здесь и есть
			// производитель отказа «каталог такого ресурса не знает».
			if rerr := w.ReplaceRuleRefs(ctx, out.ID, domain.RuleRefsOf(out.Rules)); rerr != nil {
				return fmt.Errorf("%w: %s: %w", ErrWriteFailed, r.Name, rerr)
			}
			rep.Written++
			rep.Names = append(rep.Names, string(r.Name))
		}
		return nil
	})
	if err != nil {
		return rep, err
	}
	return rep, nil
}

// roleOf — объявленная роль в форме домена. Проверка домена стоит ЗДЕСЬ, до
// писателя: отказ тогда называет роль и правило, а не приезжает SQLSTATE от
// оператора вставки.
func roleOf(mr *manifest.Role) (domain.Role, error) {
	name := domain.RoleName(mr.ID)
	rules := make(domain.Rules, 0, len(mr.Rules))
	for _, rule := range mr.Rules {
		rules = append(rules, rule.DomainRule())
	}
	compiled, cerr := domain.CompileRules(rules)
	if cerr != nil {
		return domain.Role{}, fmt.Errorf("%w: %s: %w", ErrRolePolicyNotCompilable, name, cerr)
	}
	r := domain.Role{
		ID:          domain.SystemRoleID(name),
		ClusterID:   domain.ClusterSingletonID,
		Name:        name,
		Description: domain.Description(mr.Description),
		Rules:       rules,
		Permissions: compiled,
		IsSystem:    true,
	}
	if verr := r.Validate(); verr != nil {
		return domain.Role{}, fmt.Errorf("%w: %s: %w", ErrRoleRejectedByDomain, name, verr)
	}
	return r, nil
}
