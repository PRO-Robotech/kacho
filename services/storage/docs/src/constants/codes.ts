// gRPC-коды ошибок — единый словарь для компонента <Codes />.
// kacho-storage переводит sentinel-ошибки use-case/repo в gRPC-статус через
// serviceerr.ToStatus; формат ошибки (конвенция Kachō) —
// google.rpc.Status {code, message, details:[]}.
//
// Неклассифицированная ошибка НИКОГДА не эхает текст драйвера БД: она сводится к
// фиксированному сообщению, поэтому детали подключения и SQL наружу не попадают.
export const CODES = {
  invalidArgument: {
    grpc: 'INVALID_ARGUMENT',
    http: '400',
    when: 'Некорректный аргумент: формат id, regex имени, отсутствие обязательного поля, неизвестное поле update_mask, immutable-поле в mask, размер тома меньше минимума образа',
  },
  notFound: {
    grpc: 'NOT_FOUND',
    http: '404',
    when: 'Ресурс с указанным id не существует: id корректен по форме, но строки в своей БД нет',
  },
  alreadyExists: {
    grpc: 'ALREADY_EXISTS',
    http: '409',
    when: 'Нарушение UNIQUE — дубль пары (projectId, name)',
  },
  failedPrecondition: {
    grpc: 'FAILED_PRECONDITION',
    http: '400',
    when: 'Состояние не позволяет операцию: том не READY, том уже привязан, расхождение зоны / проекта / региона, имя устройства занято, свободных имён устройств нет, тип диска держат тома; потолок квоты на вид не назван ни на одной области видимости (признак QUOTA_NOT_PROVISIONED) — требуется завести предел',
  },
  resourceExhausted: {
    grpc: 'RESOURCE_EXHAUSTED',
    http: '429',
    when: 'Исчерпана квота на число ресурсов вида: потолок назван и выбран полностью (признак QUOTA_EXCEEDED в ErrorInfo). Требуется поднять предел',
  },
  unavailable: {
    grpc: 'UNAVAILABLE',
    http: '503',
    when: 'Недоступен peer-сервис (kaname / kacho-geo) на пути запроса — отказ закрытый: мутация не выполняется, страница списка не отдаётся нефильтрованной',
  },
  unauthenticated: {
    grpc: 'UNAUTHENTICATED',
    http: '401',
    when: 'Отсутствует или невалиден токен (проверяется на api-gateway)',
  },
  permissionDenied: {
    grpc: 'PERMISSION_DENIED',
    http: '403',
    when: 'У субъекта нет нужного отношения на ресурс или проект в модели прав',
  },
  unimplemented: {
    grpc: 'UNIMPLEMENTED',
    http: '501',
    when: 'Метод объявлен контрактом, но его предмет отсутствует в этой фазе — инфраструктурная проекция тома ждёт data-plane',
  },
  internal: {
    grpc: 'INTERNAL',
    http: '500',
    when: 'Внутренняя ошибка. Текст фиксирован и не несёт деталей БД',
  },
} as const

export type CodeKey = keyof typeof CODES
