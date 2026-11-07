# https://github.com/reteps/dockerfmt/issues/71
FROM alpine
COPY ["my file.txt", "/app dir/"]
ADD ["src dir", "/opt/x y/"]
COPY ["a", "b/"]
COPY --chown=1:1 ["a b", "c"]
