# Front de cliente: HTML/CSS/JS servido por nginx, sem build.
#
# O contexto de build é a RAIZ do monorepo (como as outras imagens), por isso o
# COPY aponta para apps/site/ e não para o diretório do Dockerfile.
FROM nginx:1.27-alpine
COPY apps/site/nginx.conf /etc/nginx/conf.d/default.conf
COPY apps/site/public/ /usr/share/nginx/html/
EXPOSE 80
