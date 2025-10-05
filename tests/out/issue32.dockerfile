FROM debian:bullseye-slim
LABEL maintainer="... <...>"

COPY sources.list /etc/apt/sources.list

# Upgrade the system + Install all packages
ARG DEBIAN_FRONTEND=noninteractive
# Install all packages below :
RUN apt-get update \
    && apt-get install --no-install-recommends -y \
        ca-certificates \
        imagemagick \
        php-bcmath \
        php-curl \
        php-db \
        php-fpm \
        php-gd \
        php-imagick \
        php-intl \
        php-ldap \
        php-mail \
        php-mail-mime \
        php-mbstring \
        php-mysql \
        php-redis \
        php-soap \
        php-sqlite3 \
        php-xml \
        php-zip \
        ssmtp \
        # bind9-host iputils-ping lsof iproute2 netcat-openbsd procps strace tcpdump traceroute \
        # Clean and save space
    && rm -rf /var/lib/apt/lists/* \
    # Set timezone
    && ln -sf /usr/share/zoneinfo/Europe/Berlin /etc/localtime \
    && dpkg-reconfigure tzdata

WORKDIR /var/www/html

ADD https://.../check_mk/agents/check_mk_agent.linux /usr/bin/check_mk_agent
COPY php_fpm_pools /usr/lib/check_mk_agent/plugins/
COPY php_fpm_pools.cfg /etc/check_mk/
RUN chmod 755 /usr/bin/check_mk_agent /usr/lib/check_mk_agent/plugins/*

COPY www.conf /etc/php/7.4/fpm/pool.d/
COPY ssmtp.conf /etc/ssmtp/ssmtp.conf
COPY environment /etc/environment

RUN mkdir -m 0755 /run/php

ENV http_proxy=...
ENV https_proxy=...
ENV no_proxy=...
ENV HTTP_PROXY=...
ENV HTTPS_PROXY=...
ENV NO_PROXY=...

EXPOSE 9000

CMD ["/usr/sbin/php-fpm7.4", "--nodaemonize", "--fpm-config", "/etc/php/7.4/fpm/php-fpm.conf"]