// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package auditlistfilter states how kacho-compute is laid out for the public-List
// gate. The analysis itself — and why it parses instead of grepping — lives in
// tools/listfiltergate.
//
// # This service's shape
//
// compute authorizes the page in the TRANSPORT layer: `internal/handler` is one
// flat package, each resource has its own handler type (`InstanceHandler`,
// `MachineTypeHandler`), and its `List` runs the page it just read through the
// generic `filterVisible` helper of internal/handler/list_filter.go, which calls
// kacho-iam BatchCheck. So the resource is told apart by the receiver TYPE, and the
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

import "github.com/PRO-Robotech/kacho/tools/listfiltergate"

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
	Filters:        []string{"listnarrow.Page", "listnarrow.IDs"},
	Banned:         []string{"ListAllowedIDs", "ListObjects"},
	SubjectScopers: []string{"ListForCaller"},

	Listings: map[string]listfiltergate.Listing{
		"instance.List": {Shape: listfiltergate.RowFilter},
		"instance.ListOperations": {
			Shape: listfiltergate.ParentGate,
			Gate:  "svc.Get",
		},
		// Объявлен контрактом, реализации не несёт: привязки доступа —
		// ресурс домена iam, и поверхность на инстансе задваивала бы выдачу
		// прав. Метод переопределён рукой и отказывает с названной причиной
		// (internal/handler/declared_but_absent.go), поэтому страницы не
		// существует — сужать нечего.
		//
		// Форма проверяется по коду: гейт требует, чтобы КАЖДЫЙ возврат метода
		// отдавал nil-ответ с ошибкой. Появится путь, строящий ответ, —
		// объявление станет ложным и гейт покраснеет; уйдёт RPC из контракта —
		// у записи не станет предмета.
		"instance.ListAccessBindings": {Shape: listfiltergate.NeverServes},
		"machine_type.List": {
			Shape: listfiltergate.ClusterScoped,
			Reason: "cluster-wide sizing catalog (COMP-1 F7): every authenticated caller reads " +
				"every row and there are no per-object grants to narrow to. The exclusion expires " +
				"with its method — retire MachineTypeHandler.List and this entry becomes a finding.",
		},
	},
}
