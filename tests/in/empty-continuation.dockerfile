# https://github.com/reteps/dockerfmt/issues/59
FROM alpine

# Separators after flags and before && lines
RUN \
  --mount=type=bind,source=./a,target=/b \
  \
  echo one \
  \
  && echo two \
  \
  && echo three

# Comment before the first flag
RUN \
  # \
  --mount=type=bind,source=./a,target=/b \
  --mount=type=cache,target=/c \
  echo one

# Comment and separator between flags, comment after flags
RUN --mount=type=bind,source=./a,target=/b \
  # between flags
  --mount=type=cache,target=/c \
  \
  --mount=type=cache,target=/d \
  # after flags
  echo one \
  \
  echo two

# Separators in an argument list, before a comment, and with no flags
RUN apt-get update \
    \
    && apt-get install -y \
        curl \
        \
        git \
    \
    # comment
    && rm -rf /var/lib/apt/lists/*
RUN \
  \
  echo one

# Non-mount flags and pipes
RUN --network=host \
  \
  echo one
RUN echo a \
  \
  | grep a \
  \
  || true

# Trailing whitespace after the backslash
RUN echo a \
  \   
  && echo b

# CMD shell form
CMD echo a \
  \
  && echo b

# A "\" line with no surrounding whitespace joins words; not a separator
RUN echo good\
\
night \
  \
  && echo x
