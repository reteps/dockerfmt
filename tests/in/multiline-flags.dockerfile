# Single flag, no continuation
RUN --network=host apt-get install vim

# Single flag with continuation
RUN --mount=type=cache,target=/go/pkg/mod \
    go build -o /app .

# Multiple flags with continuations
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -o /app .

# Multiple flags on same line (should stay on same line)
RUN --network=host --security=insecure echo test

# Mixed: some on same line, continuation after
RUN --network=host --mount=type=cache,target=/cache \
    go build

# COPY with multiline flags
COPY --chown=user:group \
    --chmod=644 \
    ./src /dst

# With complex shell content after multiline flags
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -o /app . && \
    chmod +x /app
