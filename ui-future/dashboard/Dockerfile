FROM node:26-alpine AS build

WORKDIR /app

ARG KACHO_PUBLIC_BASE=/dashboard-remote/
ENV KACHO_PUBLIC_BASE=$KACHO_PUBLIC_BASE

COPY dashboard/package*.json ./
RUN npm ci

COPY dashboard/ ./
RUN npm run build

# nginx-unprivileged: работает под non-root uid 101, слушает 8080, pid и временные файлы
# уводит в /tmp — поэтому уживается с runAsNonRoot + readOnlyRootFilesystem
# (см. deploy/templates/deployment-*.yaml). Конфиг в кластере приезжает шаблоном из
# ConfigMap и рендерится в /etc/nginx/conf.d, куда деплоймент подкладывает emptyDir;
# конфиг ниже — запечённый, для одиночного `docker run`.
FROM nginxinc/nginx-unprivileged:1.31-alpine

COPY dashboard/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html

# Явный non-root USER (trivy DS-0002: контейнер не должен работать под root).
USER 101

EXPOSE 8080

CMD ["nginx", "-g", "daemon off;"]
