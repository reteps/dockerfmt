# https://github.com/reteps/dockerfmt/issues/77
FROM alpine
RUN apk add \
    curl \
    git
RUN echo a \
    && echo b
RUN apk add \
    # a comment after a blank line
    curl
