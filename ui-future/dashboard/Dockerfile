FROM node:26-alpine AS build

WORKDIR /app

ARG KACHO_PUBLIC_BASE=/dashboard-remote/
ENV KACHO_PUBLIC_BASE=$KACHO_PUBLIC_BASE

# Этот remote ставит зависимости сам (свой package-lock), но собирает и исходники
# shared/ — они лежат рядом, а не внутри его дерева.
#
# ПОЧЕМУ ЭТО ЗДЕСЬ. Модуль начал импортировать из `@shared` (граница отказа
# раздела, канон имён), и без этой строки образ собирается на дереве, где
# `shared/` нет вовсе: `tsc` не находит модуль, сборка падает. Локальная сборка
# при этом зелёная — каталог рядом, — и расхождение видно ТОЛЬКО в образе.
#
# Установка в КОРЕНЬ стадии даёт обоим деревьям один node_modules: иначе импорты
# shared/src резолвились бы вверх от /app/shared и не находили ничего, а react
# нашёлся бы дважды. Так же устроены образы compute, storage, nlb и registry.
COPY dashboard/package*.json ./
RUN npm ci

COPY shared/ shared/
COPY dashboard/ dashboard/
WORKDIR /app/dashboard
RUN npm run build

# nginx-unprivileged: работает под non-root uid 101, слушает 8080, pid и временные файлы
# уводит в /tmp — поэтому уживается с runAsNonRoot + readOnlyRootFilesystem
# (см. deploy/templates/deployment-*.yaml). Конфиг в кластере приезжает шаблоном из
# ConfigMap и рендерится в /etc/nginx/conf.d, куда деплоймент подкладывает emptyDir;
# конфиг ниже — запечённый, для одиночного `docker run`.
FROM nginxinc/nginx-unprivileged:1.31-alpine

COPY dashboard/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dashboard/dist /usr/share/nginx/html

# Явный non-root USER (trivy DS-0002: контейнер не должен работать под root).
USER 101

EXPOSE 8080

CMD ["nginx", "-g", "daemon off;"]
