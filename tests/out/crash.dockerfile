FROM nginx
WORKDIR /app
ARG PROJECT_DIR=/
ARG NGINX_CONF=nginx.conf
COPY $NGINX_CONF /etc/nginx/conf.d/nginx.conf
COPY $PROJECT_DIR /app
CMD ["/bin/sh", "-c", "mkdir --parents /var/log/nginx && nginx -g \"daemon off;\""]
