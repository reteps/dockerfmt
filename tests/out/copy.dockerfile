FROM scratch
COPY --exclude=nginx-default.conf \
    --exclude=zap-scan-automation-framework.yml \
    --exclude=renovate.json5 \
    --exclude=compose.yml \
    . .
COPY --chown=user:group ./single-line /dest
ADD --keep-git-dir \
    ./ /data/src
