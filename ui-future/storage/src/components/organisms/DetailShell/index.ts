// Проксирует ЯЗЫК общей карточки, а не один её файл.
//
// Прежде здесь стоял `export * from "./DetailShell"` — то есть наружу выходил
// один модуль ствола, и язык карточки (`DetailSurface`, `PropertyRows`,
// `PageHead`, `PAGE_PADDING`, `DETAIL_CONTENT_WIDTH`) через `@/` был
// недоступен. Модуль, которому он понадобился, был вынужден рисовать своё — и
// рисовал: «Обзор» собирался antd-таблицей с рамкой на каждой ячейке, а секции
// типа диска — собственной `<table>`. Узкий проход к общему коду не копирует
// реализацию, он ВЫНУЖДАЕТ её завести.
//
// `JsonTab` общего ствола здесь НЕ проксируется, и это не пропуск: storage его
// не зовёт (вкладку JSON `ResourceShell` собирает сам из `JsonMonacoView`), а
// его цепочка тянет `import.meta`, которого прогон проб этого модуля
// исполнить не может — суита падала бы целиком, не дойдя ни до одного
// утверждения. Понадобится — приедет вместе с починкой прогона, а не вперёд
// неё.
export * from "@shared/components/organisms/DetailShell/DetailShell";
export * from "@shared/components/organisms/DetailShell/DetailSurface";
export * from "@shared/components/organisms/DetailShell/PageHead";
