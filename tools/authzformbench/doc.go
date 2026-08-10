// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package authzformbench compares SIX shapes of the same authorization grant —
// five of them against a real OpenFGA over a real Postgres, the sixth against a
// Postgres of its own — and reports write cost, read cost and volume for each,
// as a CURVE over N rather than as one number.
//
// It is a measuring instrument, not a product change. Nothing under services/,
// pkg/, gateway/ or proto/ imports it, and it writes nothing into them: the
// canonical model proto/kacho/cloud/iam/v1/fga_model.fga is READ and transformed
// in memory. The two shapes that need a different model (C and D) carry that
// difference as a transform of the canonical text, so the model they measure
// cannot drift from the model production runs — it IS the canonical model with
// named relations added.
//
// # The subject
//
// A role rule is narrowed by a label selector and EXPANDED by the reconciler into
// tuples: one per object × verb, for each subject of the binding. There is no
// exponent — three linear factors multiply:
//
//	tuples = N objects × M verbs × S subjects
//
// At N=1000, M=5, S=10 that is 50 000 tuples from one row of a role. The question
// this package exists to answer is whether folding any of the three factors into
// an indirection is worth what the indirection costs on the READ path, which is
// hot: pages are narrowed in partitions and a page is up to 1000 objects.
//
// # The six shapes
//
//	A  flat        one tuple per object × verb × subject          N·M·S      (today)
//	B  group       grant to group#member, subjects are members    N·M + S
//	C  role-rel    one role relation per object, verbs derive it  N·S
//	D  container   objects point at a container, grant on it      N + S
//	BCD combined   container + group                              N + S + 1
//	E  relational  a row of binding + its selector; no engine     S + 2
//
// B needs NO model change: `group#member` is already an accepted subject on every
// verb of every type. C and D need relations that do not exist today, which is why
// they are transforms rather than a discipline of granting.
//
// # Форма E — не шестой способ писать кортежи, а их отсутствие
//
// Она вычисляет вердикт ЗАПРОСОМ к БД поверх того же зеркала объектов, которое
// iam ведёт сегодня, и внешнего движка отношений у неё нет. Отсюда три вещи,
// которых у прежних пяти не бывает и которые измеряются отдельно, а не сводятся
// к «быстрее»:
//
//   - выдача и отзыв живут в ТОЙ ЖЕ транзакции, что предмет выдачи. У пяти
//     прежних форм эта операция неприменима by construction — общей транзакции
//     между БД сервиса и чужим движком не существует, — и это отдельный, четвёртый
//     исход ячейки, никогда не сворачиваемый в «не выполнилось»;
//   - колонка «обращений» расщеплена на две. У движка это HTTP-вызов, за которым
//     стоит своё число запросов к его Postgres; у формы E — SQL-стейтмент. Обе
//     печатаются под своими именами и не складываются ни в одной ячейке;
//   - каскад трёх верхних уровней доступа у формы E — соединение ограниченной
//     глубины по родительским указателям (leaf → project → account → cluster).
//     Он не читает ничего, что доставляется очередью, — но того же свойства
//     держится и действующая форма, поэтому каскад спрашивается ОБЕИМ, и
//     утверждение о нём — о паритете, а не о преимуществе.
//
// Что форма E НЕ убирает и о чём отчёт обязан сказать против её интереса:
// зеркало объектов и очередь его наполнения. Она убирает материализацию доступа,
// а не зеркало; «очередей больше нет» — неверное прочтение.
//
// # What is measured, and in what unit
//
// Write, per operation, wall-clock milliseconds AND the number of store round
// trips (writes are dominated by round trips, so a millisecond figure without the
// request count hides which of the two moved):
//
//	W1 grant       materialize the whole binding over N objects
//	W2 revoke      withdraw ONE subject from the binding
//	W3 relabel-1   one object enters the selected set
//	W4 relabel-K   K objects enter the selected set
//
// Read, per operation, wall-clock milliseconds:
//
//	R1 check       one object, one verb, one subject
//	R2 page-50     one partition — the store's own BatchCheck ceiling
//	R3 page-1000   a contract-sized page: ceil(1000/50) partitions, ≤8 in flight,
//	               mirroring authzfilter.MaxBatchChecksPerRequest / BatchParallelism
//
// Volume: the exact tuple count for the store, and the logical row bytes Postgres
// reports for that store's rows. Index overhead is table-wide, not per-store, so it
// is reported once for the whole table and never attributed to a shape.
//
// # Four outcomes, not three
//
// Every cell is one of measured / refused / not-run / not-applicable. "Not-run" —
// the stack did not come up, the operation timed out, the store refused the
// request — is NEVER folded into a shape's favour and never silently omitted; it
// is carried through to the report as its own category with the reason attached. A
// shape that could not be measured did not win.
//
// Четвёртая категория — «неприменимо by construction» — заведена ради ячеек,
// которые СОДЕРЖАТЕЛЬНЫ именно своей пустотой: у движка отношений нет и не может
// быть общей транзакции с БД предмета выдачи. Свернуть её в «не выполнилось»
// значило бы напечатать «не-измеренных 20» и заставить читателя решить, что замер
// не доехал, — тогда как это самый содержательный результат таблицы. Она
// печатается своей строкой, с причиной, и сумма четырёх категорий обязана
// равняться числу ячеек.
//
// # Behavioural equivalence is a PRECONDITION, not a footnote
//
// A shape that answers differently is not faster, it is different. Before any
// timing is reported, TestFormsAnswerIdentically asks every shape the same
// question set — positives AND negatives (wrong subject, wrong object, ungranted
// verb, after revoke, after a subject leaves the group, after an object leaves the
// set) — and requires identical verdict vectors. The negatives are the half that
// matters: an additive shape that never withdraws is green on every positive.
//
// Фикстура несёт ДВУХ арендаторов, и это не полнота картины, а условие того, что
// предпосылка вообще о чём-то говорит. При одном аккаунте и одном проекте конъюнкт
// «объект лежит в области выдачи» тождественно истинен для каждого спрошенного
// объекта: его полное снятие оставляло зелёными все пробы вердикта, то есть
// эквивалентность была обеспечена на одноарендном случае, а решение принимается
// для многоарендной системы. Поэтому вопросник спрашивает про помеченный объект
// ЧУЖОГО арендатора (у формы E отказать обязана область, у форм движка отказ
// структурен), а каскад спрашивается ещё и про чужой аккаунт — иначе
// «администратор ЭТОГО аккаунта» и «администратор любого» неразличимы.
//
// # Provenance
//
// The report names the OpenFGA image, the Postgres image, the machine, the tree
// revision and the date. A measurement without them is not evidence, because it
// cannot be repeated.
//
// # Reuse, not reinvention
//
// The container form is the one this tree already uses for OpenFGA integration
// proofs — services/iam/internal/testsupport/fgatest: one server for the process,
// a STORE per case, the model transformed once by the pinned openfga/cli image.
// That package cannot be imported from here (it is under services/iam/internal/,
// so only services/iam/... may import it), so the form is reproduced rather than
// called. Where the two differ, deliberately:
//
//   - the datastore is POSTGRES, not the in-memory default. Production runs
//     postgres (deploy/helm/umbrella/values.dev.yaml: openfga.datastore.engine),
//     and the whole subject here is how many datastore round trips an indirection
//     costs. In-memory would understate C and D by construction and the result
//     would be an artefact of the harness;
//   - it is a benchmark, so it owns its timing, warm-up and repeats rather than
//     leaving them to `go test -bench`, whose adaptive iteration count fights a
//     per-iteration setup this size and which reports a mean where the spread is
//     the interesting part.
package authzformbench
