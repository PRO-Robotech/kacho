// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package engineplaces

import "sort"

// РОД ВОПРОСА. Снятие движка требует от каждого рода РАЗНОГО: вердикту и
// перечислению нужен заменитель в форме, чтению и записи хранилища кортежей —
// исчезновение адресата, метаданным хранилища — ничего, потому что это вообще не
// вопрос об отношениях. Поэтому перепись, не разложенная по роду, не отвечает на
// вопрос «что здесь делать».
//
// Родов ПЯТЬ, а не четыре: пятый — обращение к метаданным хранилища, и дерево
// его уже различает.
const (
	// KindVerdict — «имеет ли этот субъект это отношение к этому объекту».
	KindVerdict = "вердикт об объекте"
	// KindEnumerate — «перечисли объекты/субъекты/основания».
	KindEnumerate = "перечисление"
	// KindReadStore — чтение самих кортежей хранилища.
	KindReadStore = "чтение хранилища кортежей"
	// KindWriteStore — запись/снятие кортежей хранилища.
	KindWriteStore = "запись хранилища кортежей"
	// KindStoreMeta — метаданные хранилища (идентификатор модели, сведения о
	// сторе). Действующий страж дерева называет это дословно «не вопрос об
	// отношениях», и это верно: заменителя такому обращению не нужно — нужно
	// исчезновение адресата.
	KindStoreMeta = "метаданные хранилища"
	// KindPlumbing — НЕ вопрос: внутренняя механика адаптера (транспорт,
	// сроки, наблюдение попытки). Род объявлен отдельно, чтобы каждый метод
	// якорного типа имел ровно один род и «метод без рода» осталось находкой,
	// а не молчаливой дырой в таксономии.
	KindPlumbing = "механика адаптера"
)

// Kinds — объявленный перечень родов. Порядок — от вопроса к механике.
func Kinds() []string {
	return []string{
		KindVerdict, KindEnumerate, KindReadStore, KindWriteStore,
		KindStoreMeta, KindPlumbing,
	}
}

// QuestionKinds — роды, которые СУТЬ вопрос к движку (их пять). Механика
// адаптера сюда не входит: у неё нет заменителя, потому что нет предмета.
func QuestionKinds() []string {
	return []string{KindVerdict, KindEnumerate, KindReadStore, KindWriteStore, KindStoreMeta}
}

// methodKind — род каждого метода якорного типа.
//
// ТАБЛИЦА КЛЮЧУЕТСЯ ИМЕНЕМ, А ПЕРЕЧЕНЬ МЕТОДОВ ВЫВОДИТСЯ ИЗ ТИПА — и это
// разные вещи. Выписанный перечень РОДОВ разошёлся бы с типом молча: метод,
// добавленный клиенту движка и не внесённый сюда, попадает в
// `Census.UnclassifiedMethods` и роняет гейт. Действующий в дереве страж этого
// класса держит список из тринадцати имён и уже разошёлся со своим предметом
// именно потому, что перечня у него нет — он его выписывает.
var methodKind = map[string]string{
	// вердикт об объекте
	"Check":                      KindVerdict,
	"CheckConsistent":            KindVerdict,
	"CheckWithContext":           KindVerdict,
	"CheckWithContextConsistent": KindVerdict,
	"CheckWithContextualTuples":  KindVerdict,
	"BatchCheckItems":            KindVerdict,
	"BatchCheckWithContext":      KindVerdict,
	"check":                      KindVerdict,
	"checkWithContext":           KindVerdict,

	// перечисление
	"ListObjects":     KindEnumerate,
	"ListSubjects":    KindEnumerate,
	"ListUsers":       KindEnumerate,
	"listUsersOfType": KindEnumerate,
	"Expand":          KindEnumerate,

	// чтение хранилища кортежей
	"ReadTuples":       KindReadStore,
	"ReadTuplesStrong": KindReadStore,
	"readTuples":       KindReadStore,
	"fgaRead":          KindReadStore,
	"fgaReadOnce":      KindReadStore,
	"readGrant":        KindReadStore,

	// запись хранилища кортежей
	"WriteTuples":            KindWriteStore,
	"DeleteTuples":           KindWriteStore,
	"WriteConditionalTuples": KindWriteStore,
	"writeOrDelete":          KindWriteStore,
	"writeOrDeleteChunked":   KindWriteStore,
	"applyBatch":             KindWriteStore,
	"applyEachTuple":         KindWriteStore,
	"completeGrant":          KindWriteStore,

	// метаданные хранилища
	"GetStoreInfo":               KindStoreMeta,
	"LatestAuthorizationModelID": KindStoreMeta,
	"latestModel":                KindStoreMeta,

	// механика адаптера — не вопрос
	"do":                KindPlumbing,
	"observeAttempt":    KindPlumbing,
	"checkTimeout":      KindPlumbing,
	"listTimeout":       KindPlumbing,
	"writeTimeout":      KindPlumbing,
	"batchCheckTimeout": KindPlumbing,
}

// MethodKind — один метод якорного типа и его род.
type MethodKind struct {
	Method string `json:"method"`
	Kind   string `json:"kind"`
}

// classifyMethods раскладывает ПЕРЕЧЕНЬ, ВЫВЕДЕННЫЙ ИЗ ТИПА, по родам и
// возвращает отдельно те методы, для которых рода нет.
func classifyMethods(names []string) (classified []MethodKind, unclassified []string) {
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	for _, n := range sorted {
		k, ok := methodKind[n]
		if !ok {
			unclassified = append(unclassified, n)
			continue
		}
		classified = append(classified, MethodKind{Method: n, Kind: k})
	}
	return classified, unclassified
}
