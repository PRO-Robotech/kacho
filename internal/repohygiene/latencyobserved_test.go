// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// latencyobserved_test.go — процесс, поднимающий gRPC-слушателя, ОБЯЗАН
// наблюдать задержку обслуженного вызова.
//
// # Предмет
//
// Пока гистограмму задержки заводит каждый сервис у себя, «не завёл» неотличимо
// от «завёл такую же»: отсутствие серии ничего не печатает, слушатель
// поднимается молча, и узнают об этом ровно тогда, когда задержку понадобилось
// посмотреть, — то есть в разборе происшествия, когда данных уже не будет.
//
// # Почему поверхностей ВОСЕМЬ, а не семь
//
// Слушателей платформы поднимают семь сервисов и КРАЙ. Край — восьмая
// поверхность и вторая, которая строит серверы сама; он же стоит перед КАЖДЫМ
// обращением арендатора, поэтому без его ряда картина неполна ровно в том месте,
// куда смотрят первым. Обход, читающий только `services/`, объявил бы «восемь из
// восьми» по семи — то есть был бы верен и бесполезен одновременно.
//
// # Почему гейт, если есть отказ старта
//
// Отказ старта О13 (`servicecontract.New`) связывает того, кто отдал входящий
// путь НОСИТЕЛЮ: носитель заводит измеритель сам, и слушателя без него у него
// не бывает. Но конструктор сервера (`grpcsrv.NewServer`) публичен, и сервис
// вправе поднять слушателя МИМО носителя — тогда ни одно из двух построений его
// не касается. Такой сервис в дереве есть, и он самый нагруженный.
//
// Отсюда предикат: судится тот, кто строит сервер САМ. Отдавший себя носителю
// наблюдает задержку по построению и в находки не попадает — иначе гейт требовал
// бы у всех шести повторить руками то, что за них уже сделано, а перечень
// «кому можно не» стал бы местом, куда вписывают исключения.
//
// # Разбор синтаксического дерева, а не текста
//
// Оба имени встречаются в комментариях, объясняющих ровно эту провязку.
// Текстовый поиск принял бы объяснение за исполнение — тот самый класс, который
// гейт и ловит. Локальное имя пакета берётся из ОБЪЯВЛЕНИЯ импорта: псевдоним не
// должен становиться слепым пятном.

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// Как поднимают слушателя и как заводят измеритель. Разъедутся с кодом —
// перепись найдёт ноль подъёмов, и гейт скажет об этом отдельной строкой, а не
// промолчит.
const (
	serverPkg   = "github.com/PRO-Robotech/kacho/pkg/grpcsrv"
	serverFunc  = "NewServer"
	latencyFunc = "NewServerLatency"

	// grpcPkg — БИБЛИОТЕЧНЫЙ конструктор сервера.
	//
	// Он здесь не для полноты: край строит оба своих слушателя именно им, минуя
	// и носитель входящего пути, и общий конструктор платформы. Предикат,
	// знающий только своё имя, объявил бы «восемь из восьми» по семи — то есть
	// был бы верен и бесполезен одновременно. Это и произошло в первой редакции
	// гейта: край читался обходом, но раисером не считался.
	grpcPkg = "google.golang.org/grpc"
)

// listenerRaiser — как сервис поднимает слушателя и где он это делает.
type listenerRaiser struct {
	viaCarrier string // координата вызова носителя, если он есть
	direct     string // координата собственного конструктора сервера
	measurer   string // координата сборки измерителя задержки
}

// TestEveryGRPCListenerObservesItsLatency — слушатель, поднятый мимо носителя,
// наблюдает задержку сам.
func TestEveryGRPCListenerObservesItsLatency(t *testing.T) {
	root := repoRoot(t)
	raisers, scanned, err := scanLatencyObservers(root)
	if err != nil {
		t.Fatalf("%v", err)
	}

	names := make([]string, 0, len(raisers))
	for svc := range raisers {
		names = append(names, svc)
	}
	sort.Strings(names)

	var carrier, direct, directObserving []string
	for _, svc := range names {
		r := raisers[svc]
		if r.viaCarrier != "" {
			carrier = append(carrier, svc)
		}
		if r.direct != "" {
			direct = append(direct, svc)
			if r.measurer != "" {
				directObserving = append(directObserving, svc)
			}
		}
	}
	// Перепись печатается ВСЕГДА: «ноль находок» обязано быть отличимо от «ноль
	// прочитанного».
	t.Logf("осмотрено: файлов прочитано=%d, поверхностей со слушателем=%d (%s); "+
		"через носитель=%d (%s) — наблюдают задержку по построению; "+
		"строят сервер сами=%d (%s), из них заводят измеритель=%d (%s)",
		scanned, len(names), strings.Join(names, ", "),
		len(carrier), strings.Join(carrier, ", "),
		len(direct), strings.Join(direct, ", "),
		len(directObserving), strings.Join(directObserving, ", "))

	// Предпосылка: слушателей кто-то поднимает. Ноль означает, что конструктор
	// переименован либо сервисы съехали, — и тогда гейт судит пустоту.
	if len(names) == 0 {
		t.Fatalf("предпосылка гейта нарушена: ни одна поверхность не поднимает gRPC-слушателя — "+
			"ни через носитель (%s.%s), ни своим конструктором (%s.%s). Имя переехало либо "+
			"сервисы съехали; пока это не выяснено, гейт не судит ничего (файлов прочитано %d)",
			filepath.Base(carrierPkg), carrierFunc, filepath.Base(serverPkg), serverFunc, scanned)
	}

	for _, svc := range names {
		r := raisers[svc]
		if r.direct == "" || r.measurer != "" {
			continue
		}
		t.Errorf("%s поднимает gRPC-слушателя СВОИМ конструктором (%s, %s.%s), минуя "+
			"носитель входящего пути, и нигде не заводит измеритель задержки (%s.%s). Значит его "+
			"слушатель служит и ни одной длительности не выпускает наружу: «отвечает за "+
			"миллисекунду» и «отвечает за десять секунд» снаружи выглядят одинаково, а регрессию "+
			"нечем ни увидеть, ни опровергнуть. Отказ старта О13 сюда не достаёт — он связывает "+
			"того, кто отдал входящий путь носителю",
			svc, r.direct, filepath.Base(serverPkg), serverFunc,
			filepath.Base(serverPkg), latencyFunc)
	}
}

// scanLatencyObservers — обход дерева сервисов: кто поднимает слушателя и кто
// заводит измеритель задержки.
//
// Состав берётся у индекса git: обход диска прочитал бы игнорируемое, и вердикт
// стал бы свойством рабочего каталога, а не коммита.
func scanLatencyObservers(root string) (raisers map[string]listenerRaiser, files int, err error) {
	// Оба корня, а не один: край серверы строит сам и в `services/` не лежит.
	// Отсутствие второго корня — не «в нём ничего нет», а «его не читали»,
	// поэтому корень, которого нет в дереве, пропускается молча только если он
	// действительно отсутствует, а не если обход по нему отказал.
	var tracked []string
	roots := 0
	for _, top := range []string{"services", "gateway"} {
		dir := filepath.Join(root, top)
		if _, serr := os.Stat(dir); errors.Is(serr, fs.ErrNotExist) {
			// Корня нет в этом дереве — законно для синтетики инъекции. Это НЕ то
			// же, что «корень есть и не читается»: второе остаётся отказом, иначе
			// «ноль находок» стало бы неотличимо от «ноль прочитанного».
			continue
		}
		under, terr := treecorpus.Under(dir)
		if terr != nil {
			return nil, 0, fmt.Errorf("состав дерева под %s не читается: %w", dir, terr)
		}
		tracked = append(tracked, under...)
		roots++
	}
	if roots == 0 {
		return nil, 0, fmt.Errorf("ни одного корня со слушателями не найдено под %s "+
			"(искали services/ и gateway/) — обходу нечего читать", root)
	}
	raisers = map[string]listenerRaiser{}
	fset := token.NewFileSet()
	for _, abs := range tracked {
		if !strings.HasSuffix(abs, ".go") || strings.HasSuffix(abs, "_test.go") {
			continue
		}
		rel, rerr := filepath.Rel(root, abs)
		if rerr != nil {
			return nil, 0, fmt.Errorf("относительный путь для %s: %w", abs, rerr)
		}
		// Единица счёта — ПОВЕРХНОСТЬ: сервис под `services/<имя>/` либо край
		// целиком. Край одной единицей, а не по каталогам: его слушателей два, но
		// процесс один, и измеритель у него один на процесс.
		parts := strings.Split(filepath.ToSlash(rel), "/")
		var svc string
		switch {
		case parts[0] == "gateway":
			svc = "gateway"
		case parts[0] == "services" && len(parts) >= 2:
			svc = parts[1]
		default:
			continue
		}
		files++
		f, perr := parser.ParseFile(fset, abs, nil, parser.SkipObjectResolution)
		if perr != nil {
			return nil, 0, fmt.Errorf("разбор %s: %w", rel, perr)
		}
		r := raisers[svc]
		if callsCarrierServe(f) && r.viaCarrier == "" {
			r.viaCarrier = filepath.ToSlash(rel)
		}
		if (callsPkgFunc(f, serverPkg, serverFunc) || callsPkgFunc(f, grpcPkg, serverFunc)) &&
			r.direct == "" {
			r.direct = filepath.ToSlash(rel)
		}
		if callsPkgFunc(f, serverPkg, latencyFunc) && r.measurer == "" {
			r.measurer = filepath.ToSlash(rel)
		}
		if r.viaCarrier != "" || r.direct != "" || r.measurer != "" {
			raisers[svc] = r
		}
	}
	// Поверхность, которая измеритель завела, а слушателя не поднимает, слушателем
	// не становится: перечень судимых — это подъёмы, а не наблюдения.
	for svc, r := range raisers {
		if r.viaCarrier == "" && r.direct == "" {
			delete(raisers, svc)
		}
	}
	return raisers, files, nil
}
