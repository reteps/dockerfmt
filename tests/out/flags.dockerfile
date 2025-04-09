# https://github.com/jessfraz/dockfmt/issues/23
FROM --platform=linux/arm64 debian

RUN --network=host apt-get install vim
RUN --security echo "test"
COPY --chown=my-user:my-group --chmod=644 ./config.conf /data/config.conf
COPY --link ./another-file /data/linked-file
ADD --keep-git-dir ./ /data/src
