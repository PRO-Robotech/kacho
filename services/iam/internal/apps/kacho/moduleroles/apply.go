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
//     глаголов и проекцию сегментов МОЛЧА. Отзыв роли — предмет #1913, и он
//     выражается ПОМЕТКОЙ снятия, а не удалением строки
//     (`services/iam/docs/engineering/architecture/role-withdrawal-is-a-mark.md`);
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
//
// # Правила роли ПРОИЗВОДИТ ЭКСПОРТЁР, а не перевод манифеста (#1998)
//
// Форм записи права две, и к классу сводится только ПРОВЕРЕННЫЙ поимённый
// перечень: сведение требует каталога прав, которого у загрузчика нет. Значит у
// поимённой формы ровно один законный производитель — `roleexport.ExportRoleRules`,
// и применитель берёт правила у него.
//
// Здесь стоял прямой перевод (`manifest.Rule.DomainRule()`), и поимённое право
// он отдавал ПУСТЫМ. Дырой это не было — домен отвергает правило без глаголов, —
// но отказ говорил не о том: «политика роли не компилируется» посылает автора
// править форму правила, тогда как править надо перечень. И, что дороже: у
// полноты перечня не было читателя на пути ПРИМЕНЕНИЯ вовсе — она жила в
// исполнителе обхода дерева, а работающий процесс её не спрашивал.
//
// Следствие, названное прямо: `DomainRule()` перестал быть путём поимённой
// формы by construction — через применитель она проходит уже сведённой либо не
// проходит вовсе.
package moduleroles

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/grpc/codes"

	"github.com/PRO-Robotech/kacho/services/iam/internal/authzmap"
	"github.com/PRO-Robotech/kacho/services/iam/internal/domain"
	"github.com/PRO-Robotech/kacho/services/iam/internal/manifest"
	"github.com/PRO-Robotech/kacho/services/iam/internal/manifest/roleexport"
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
	// ErrNamedRightIncomplete — поимённый перечень права роли не полон по своему
	// классу, и роль не произведена ЦЕЛИКОМ.
	//
	// Отдельный сентинел, а не «политика не компилируется»: у них разные
	// починки. Несворачиваемость правит ФОРМУ правила, неполнота — ПЕРЕЧЕНЬ, и
	// один отказ на два предмета отправил бы автора не туда.
	ErrNamedRightIncomplete = errors.New(
		"moduleroles: declared role named right is incomplete for its class")
	// ErrRightsExportNotWired — производитель правил роли не провязан.
	//
	// Отказ, а не пропуск: без экспортёра поимённое право либо не сводится, либо
	// сводится молча, и оба исхода означают право, отличное от просимого.
	// Виновата ПРОВЯЗКА, а не вход, поэтому полоса своя — следующий шаг у этих
	// двух отказов разный.
	ErrRightsExportNotWired = errors.New(
		"moduleroles: role rules producer is not wired")
)

// Сентинелы выше — ВТОРОЕ лицо отказа, для вызывающего в процессе. Первое —
// признак полосы в `google.rpc.ErrorInfo`, и оно единственное переживает
// приведение к статусу (`refusal.go`). Спрашивать полосу — `RefusalLane`: он
// знает оба источника, и вызывающему не надо знать, на каком берегу он стоит.

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

// RightsExport — ПРОИЗВОДИТЕЛЬ правил роли из манифеста. Порт объявлен ЗДЕСЬ, в
// use-case: он описывает потребность применителя, а не каталог прав.
//
// Возвращает правила по идентификатору роли и находки. Роль, чьё право
// произвести нельзя, в карту НЕ ПОПАДАЕТ ЦЕЛИКОМ — частичное производство дало
// бы роль ýже объявленной, и отличить её от работающей можно было бы только
// вызовом.
type RightsExport interface {
	ExportRoleRules(m *manifest.Manifest) (rules map[string][]domain.Rule, faults []error)
}

// Applier — применитель. Состояния не держит: повторный прогон — штатный режим.
type Applier struct {
	tx     TxRunner
	rights RightsExport
}

// NewApplier собирает применитель над исполнителем транзакций и производителем
// правил.
//
// Производитель — параметр КОНСТРУКТОРА, а не необязательное дополнение: без
// него применитель обязан отказать, и делать отказ следствием забытого вызова
// «с...» значило бы держать режим, в котором проверка полноты снята молча.
func NewApplier(tx TxRunner, rights RightsExport) *Applier {
	return &Applier{tx: tx, rights: rights}
}

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

	// Производитель правил спрашивается ПЕРВЫМ: без него ни одна форма права до
	// строки роли не доезжает в том виде, в каком её объявили, — а отказ по
	// дороге сказал бы о форме правила вместо провязки.
	if a.rights == nil {
		werr := fmt.Errorf("%w: %s", ErrRightsExportNotWired, m.Module)
		return rep, refuse(codes.FailedPrecondition, werr.Error(),
			LaneRightsExportNotWired, m.Module, "", werr)
	}
	exported, faults := a.rights.ExportRoleRules(m)

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
		rules, produced := exported[mr.ID]
		if !produced {
			// Роль отвергнута производителем целиком. Находки СВОЕЙ роли и есть
			// отказ: они называют недостающие имена, а собственного текста у
			// применителя тут быть не может — перечень классов знает экспортёр.
			return rep, namedRightRefusal(m.Module, mr.ID, faults)
		}
		r, err := roleOf(m.Module, mr, rules)
		if err != nil {
			return rep, err
		}
		declared = append(declared, r)
	}
	rep.Declared = len(declared)
	if rep.Declared == 0 {
		return rep, nil
	}

	// failed — чья запись отказала. Величина полосы, а не украшение: без неё
	// вызывающий, применивший манифест с десятком ролей, узнаёт что «одна из них»
	// не легла. Имя известно ЗДЕСЬ и нигде больше: исполнитель транзакций о ролях
	// не знает, а восстанавливать имя разбором прозы — ровно то, против чего
	// полоса и заводится.
	var failed domain.RoleName
	err := a.tx.RunInWriteTx(ctx, func(ctx context.Context, w RoleWriter) error {
		for _, r := range declared {
			out, changed, uerr := w.UpsertSystemRole(ctx, r)
			if uerr != nil {
				failed = r.Name
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
				failed = r.Name
				return fmt.Errorf("%w: %s: %w", ErrWriteFailed, r.Name, rerr)
			}
			rep.Written++
			rep.Names = append(rep.Names, string(r.Name))
		}
		return nil
	})
	if err != nil {
		// Всякий отказ ОТСЮДА принадлежит полосе писателя by construction:
		// проверки применителя стоят выше, до открытия транзакции. Отказ самого
		// исполнителя (открытие, коммит) — тоже отказ записи, и `failed` у него
		// пуст: имени роли у него нет, и выдумывать его нечем.
		return rep, writeRefusal(m.Module, string(failed), err)
	}
	return rep, nil
}

// roleOf — объявленная роль в форме домена. Проверка домена стоит ЗДЕСЬ, до
// писателя: отказ тогда называет роль и правило, а не приезжает SQLSTATE от
// оператора вставки.
//
// Правила приходят ПАРАМЕТРОМ, от производителя (#1998), а не переводятся здесь:
// поимённая форма сводится к классу только после проверки полноты, и второй
// перевод разошёлся бы с первым молча — оба отвечают одинаково на форме классов,
// то есть на всяком входе, который сюда доезжал раньше.
//
// Полоса называется ЗДЕСЬ ЖЕ, а не восстанавливается вызывающим: обе причины
// известны в точке отказа, и разбирать собственный сентинел позже значило бы
// заводить классификатор своего же кода — с корзиной «прочее», которая молча
// приняла бы третью причину, если она когда-нибудь появится.
//
// Класс — INVALID_ARGUMENT: негодна ВХОДЯЩАЯ строка манифеста, а не состояние
// платформы. Полоса писателя отвечает FAILED_PRECONDITION, поэтому даже клиент,
// не читающий деталей, различает эти два отказа кодом.
//
// # Параметр `module` — ВЛАДЕЛЕЦ роли и адресат отказа, но НЕ предикат выдачи
//
// С задачи #1032 он попадает в строку роли: `OwnerModule` — носитель владения,
// из которого выводится политика послабления подстановки ([domain.PolicyOfRole]).
// До неё послабление было следствием кластерного якоря, и роль модуля получала
// его вместе с системностью — то есть прямой путь к `*.*.*`.
//
// Сравнения с модулем ПРАВИЛА здесь по-прежнему нет ни одной строкой, и его тут
// быть не должно. Владение ВЫДАЧЕЙ судит ЗАГРУЗЧИК —
// `manifest.ErrRoleRuleForeignModule` (задача #1902), — то есть раньше и с
// координатой `roles[i].rules[j].module`, которой у применителя нет: узла
// документа он не держит. Вторая проверка того же предмета разошлась бы с первой
// молча — обе отвечают одинаково на законном входе.
//
// Различие двух предметов названо, чтобы его не свели: #1902 отвечает на «вправе
// ли модуль раздавать права в ЧУЖОМ домене» (не вправе), #1032 — на «докуда
// достаёт ПОДСТАНОВКА в роли, которой модуль владеет» (до границы его модуля).
func roleOf(module string, mr *manifest.Role, produced []domain.Rule) (domain.Role, error) {
	name := domain.RoleName(mr.ID)
	rules := domain.Rules(produced)
	compiled, cerr := domain.CompileRules(rules)
	if cerr != nil {
		werr := fmt.Errorf("%w: %s: %w", ErrRolePolicyNotCompilable, name, cerr)
		return domain.Role{}, refuse(codes.InvalidArgument, werr.Error(),
			LanePolicyNotCompilable, module, string(name), werr)
	}
	r := domain.Role{
		ID:          domain.SystemRoleID(name),
		ClusterID:   domain.ClusterSingletonID,
		Name:        name,
		Description: domain.Description(mr.Description),
		Rules:       rules,
		Permissions: compiled,
		IsSystem:    true,
		// Владелец — модуль МАНИФЕСТА, и другого источника у него нет. Отсюда
		// политика роли становится МОДУЛЬНОЙ: подстановка ресурса законна ровно
		// в пределах этого модуля, подстановка модуля — не законна вовсе.
		OwnerModule: module,
	}
	// Набор модулей — КАНОН: применитель исполняется на старте, до того как
	// снимок каталога наполнен, и роль модуля объявлена ДЕРЕВОМ, а не строкой
	// (#1927). Живость модуля судит путь запроса, где она наблюдаема.
	if verr := r.Validate(domain.ModuleSetOf(authzmap.CatalogSeedModules()...)); verr != nil {
		werr := fmt.Errorf("%w: %s: %w", ErrRoleRejectedByDomain, name, verr)
		return domain.Role{}, refuse(codes.InvalidArgument, werr.Error(),
			LaneRejectedByDomain, module, string(name), werr)
	}
	return r, nil
}

// namedRightRefusal — отказ полосы неполного поимённого перечня.
//
// Текст собирается из находок ИМЕННО ЭТОЙ роли: находки соседних принадлежат
// своим ролям, и приложить их сюда значило бы назвать автору починку, которая
// его роли не касается. Если находки по роли нет ни одной, отказ говорит об
// этом прямо — молчаливое «роль не произведена» без причины отправило бы
// читателя искать её перебором.
func namedRightRefusal(module, roleID string, faults []error) error {
	var own []string
	for _, f := range faults {
		var finding roleexport.Finding
		if errors.As(f, &finding) && finding.Role == roleID {
			own = append(own, finding.Detail)
		}
	}
	detail := strings.Join(own, "; ")
	if detail == "" {
		detail = fmt.Sprintf("роль %q не произведена производителем правил, и находки по ней "+
			"не названо ни одной", roleID)
	}
	werr := fmt.Errorf("%w: %s: %s", ErrNamedRightIncomplete, roleID, detail)
	return refuse(codes.InvalidArgument, werr.Error(),
		LaneNamedRightIncomplete, module, roleID, werr)
}
