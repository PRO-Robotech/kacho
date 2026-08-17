// Прослойка: реализация одна и живёт в `shared/`. Копия здесь была бы форком —
// правка, севшая в неё, молча минует второе приложение, которое тот же раздел
// маршрутизирует (#447).
export { AdminLayout, default } from "@shared/components/organisms/AdminLayout";
