FROM hackernews
# https://news.ycombinator.com/item?id=43630653
ENTRYPOINT service ssh restart && bash

ENTRYPOINT sh -c 'service ssh restart && bash'

# https://github.com/reteps/dockerfmt/issues/20
FROM nginx
ENTRYPOINT ["nginx", "-g", "daemon off;"]

# https://github.com/reteps/dockerfmt/issues/20
FROM nginx
ENTRYPOINT nginx -g 'daemon off;'