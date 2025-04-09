# https://github.com/reteps/dockerfmt/issues/1#issuecomment-2785329824
FROM node:lts-alpine          as       builder

# 安装与编译代码
COPY        .      /app
WORKDIR         /app
RUN yarn           --frozen-lockfile &&            \
    yarn       build &&           \
    find . -name      '*.map' -type f -exec rm -f {} \;

# 最终的应用
FROM          abiosoft/caddy
COPY --from=builder          /app/packages/ufc-host-app/build /srv
EXPOSE      2015