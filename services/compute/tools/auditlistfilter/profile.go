// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package auditlistfilter states how kacho-compute is laid out for the public-List
// gate. The analysis itself — and why it parses instead of grepping — lives in
// pkg/listfiltergate.
//
// # This service's shape
//
// compute authorizes the page in the TRANSPORT layer: `internal/handler` is one
// flat package, each resource has its own handler type (`InstanceHandler`,
// `MachineTypeHandler`), and its `List` runs the page it just read through the
// generic `filterVisible` helper of internal/handler/list_filter.go, which calls
// kaname BatchCheck. So the resource is told apart by the receiver TYPE, and the
// proof of filtering is a call reachable from the List body.
//
// # What the previous gate keyed on, and why that was two defects
//
// It searched internal/handler/*_handler.go for the literal text
// `func (h *…Handler) List(` — that is, for what the receiver VARIABLE was NAMED —
// and then searched the whole FILE for `filterVisible|authzfilter.Filter`. Both
// halves were wrong, in opposite directions:
//
//   - renaming the receiver `h` → `hd` removed the resource from the gate's view
//     entirely, so whatever its List did afterwards went unjudged;
//   - the file-wide search accepted `authzfilter.Filter` occurring as the handler
//     struct's FIELD TYPE. Deleting the filter call from List therefore left the
//     gate green: the token it looked for was in the struct declaration a few lines
//     above, and it had nothing to do with what List did.
//
// On top of both, a missing internal/handler exited 0 with a message on stderr, so
// "the gate could not find the tree" and "the tree is clean" were the same verdict.
//
// # Declarations
//
// machine_type is the cluster-wide sizing catalog (COMP-1 F7): every authenticated
// caller is meant to read every row, there are no per-object grants to narrow to.
// It used to be excluded with --allow=machine_type — an exclusion on the RESOURCE,
// which would silently have covered any further listing method added to that
// handler. It is now declared per METHOD, with its reason where the gate can read
// it. (The previous gate also excluded Region, Zone and DiskType, all three long
// since moved to kacho-geo and kacho-storage: entries excluding nothing and standing
// ready to hide a future resource of the same name.)
//
// ListOperations was invisible to the previous gate, which matched the method name
// `List` exactly. It is declared ParentGate: the handler reads the instance through
// svc.Get and returns on its error BEFORE the operation page is read, so a caller
// who cannot see the instance does not get its operation history. The further
// narrowing inside the use-case (operations.ListForCaller, by the authenticated
// caller) lives in another package and is therefore outside this analyser's walk —
// the gate asserts what it can actually see, which is the gate preceding the read.
package auditlistfilter

import "github.com/PRO-Robotech/kacho/pkg/listfiltergate"

// quotaIsAProjectProperty — почему у чтения квот сужать НЕЧЕГО.
//
// Сужение отвечает на вопрос «какие из этих объектов доступны вызывающему», и он
// осмыслен, пока у строк ответа есть ИНДИВИДУАЛЬНЫЕ владельцы. У квоты их нет:
// это свойство проекта, как его имя или метки. Проект либо читаем этим
// вызывающим, либо нет — ровно один вопрос, и его решает `viewer` на проекте
// через извлечение области действия на крае, а не построчная проверка.
//
// Форма названа `ClusterScoped` потому, что других у гейта подходящих нет, а
// вторая (`RowFilter`) описывала бы проверку, которая ничего не отсекает. Имя
// формы здесь шире её смысла: ответ project-scoped, cluster-scoped он не
// становится — и это сказано вслух, чтобы следующий читатель не вывел из имени,
// будто квоты видны всему кластеру.
//
// Запись истекает со своим методом: снимите RPC — и она станет находкой.
const quotaIsAProjectProperty = "Quota rows are a property of the project, not objects with " +
	"individual owners: there is nothing to narrow to. The project-scope Check at the edge " +
	"(viewer on project_id) is what settles access, and the proto carries it. Named " +
	"ClusterScoped only because the gate has no third shape — the answer stays project-scoped."

// Profile describes kacho-compute to the analyser.
var Profile = listfiltergate.Profile{
	Service:    "compute",
	AnchorRoot: "internal/handler",
	// One package, several handler types: the TYPE tells the resources apart.
	PerPackage:     false,
	ReceiverSuffix: "Handler",
	// filterVisible is the service's own generic helper (internal/handler/
	// list_filter.go); FilterVisibleIDs is the port it calls, accepted so a handler
	// that talks to the port directly still counts.
	Filters: []string{"listnarrow.Page", "listnarrow.IDs"},
	Banned:  []string{"ListAllowedIDs", "ListObjects"},
	// Where the ban actually comes FROM (#684). Until this was declared the ban was
	// the two hand-written names and nothing else, and the gate said so on every run:
	// "no enumeration source declared". That line was not a warning about a defect —
	// it was a statement that the service was UNWATCHED for the form that lived in
	// iam for months before #651 found it.
	//
	// Neither surface named here enumerates today, and that is the point: a list of
	// names refuses only the forms someone has already met, so the ban has to arrive
	// BEFORE the first caller. The method set of each is read on every run; the first
	// method added to either that answers with a set of identifiers is banned inside
	// every narrowing listing the day it is written, without anyone editing a list.
	//
	// Copying iam's answer here would have been wrong twice over: iam's sources are
	// its own — the store's port and a paged verdict resolver over its own tables —
	// and kacho-compute has neither. It holds grants nowhere; every authorization
	// question it asks goes to kaname, through the ports below.
	EnumerationSources: []listfiltergate.EnumerationSource{
		// The service's own client to kaname: "may this subject act on this
		// object" (InternalIAMService.Check). One method along from it is "which
		// objects may this subject act on", and that is the form the ban must
		// already refuse when it is written.
		{Dir: "internal/check", Type: "IAMCheckClient", Role: listfiltergate.AsksVerdicts},
		// The SHARED narrow port to kaname's AuthorizeService, resolved from the
		// module root because it is foundation rather than service code. It is the
		// shortest path from "narrow this page" to "enumerate the universe": the RPC
		// it fronts is the one that enumerates (AuthorizeService.ListObjects), so a
		// profile watching only its own client would leave the likelier door unwatched.
		{Dir: "pkg/listnarrow", Type: "AuthorizeClient", Role: listfiltergate.AsksVerdicts, Shared: true},
	},
	SubjectScopers: []string{"ListForCaller"},

	Listings: map[string]listfiltergate.Listing{
		"instance.List": {Shape: listfiltergate.RowFilter},
		"instance.ListOperations": {
			Shape: listfiltergate.ParentGate,
			Gate:  "svc.Get",
		},
		// Здесь стояло объявление для перечисления привязок доступа с формой
		// «никогда не обслуживает». Волна 1 сняла сам метод с контракта —
		// привязки доступа принадлежат домену управления доступом, — и запись
		// осталась без предмета.
		//
		// Гейт это и обнаружил, ровно как обещал последней фразой прежнего
		// комментария: «уйдёт RPC из контракта — у записи не станет предмета».
		// Послабление истекло от факта, а не от чьей-то памяти, и снято тем же
		// изменением, что сняло его предмет.
		// Ключ входа в машину. Страница читается из своей БД и сужается по тому же
		// отношению, каким гейтится одиночное чтение: право проекта не отвечает на
		// вопрос «можно ли этому вызывающему видеть ЭТИ строки».
		"guest_access_key.List": {Shape: listfiltergate.RowFilter},
		"guest_access_key.ListOperations": {
			Shape: listfiltergate.ParentGate,
			Gate:  "svc.Get",
		},
		// Группа размещения: та же форма, что у ключа входа.
		"placement_group.List": {Shape: listfiltergate.RowFilter},
		"placement_group.ListOperations": {
			Shape: listfiltergate.ParentGate,
			Gate:  "svc.Get",
		},
		"quota.List": {Shape: listfiltergate.ClusterScoped, Reason: quotaIsAProjectProperty},

		"machine_type.List": {
			Shape: listfiltergate.ClusterScoped,
			Reason: "cluster-wide sizing catalog (COMP-1 F7): every authenticated caller reads " +
				"every row and there are no per-object grants to narrow to. The exclusion expires " +
				"with its method — retire MachineTypeHandler.List and this entry becomes a finding.",
		},
	},
}
