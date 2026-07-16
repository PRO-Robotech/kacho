FROM node:22-alpine AS build

WORKDIR /app

ARG KACHO_PUBLIC_BASE=/storage-remote/
ENV KACHO_PUBLIC_BASE=$KACHO_PUBLIC_BASE

COPY storage/package*.json ./
RUN npm ci

COPY storage/ ./
RUN npm run build

FROM nginx:1.27-alpine

COPY storage/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
