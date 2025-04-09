ENV a=1 \
  b=2 \
      # comment
  c=3 \
  d=4 \
  # comment
  e=5

MAINTAINER Jean Luc Picard <picardj@starfleet.gov>

FROM debian:12.6-slim

RUN set -eux; for x in {1..3}; do echo 'foo'; echo 'bar'; echo "$x"; done

RUN <<EOF
    set -eux
    for x in {1..3}
    do
        echo 'foo'
        echo 'bar'
        echo "$x"
    done
EOF

FROM node:20.9.0-alpine
RUN if [[ x$LATEST_NPM = xtrue ]]; then yarn global add npm@latest; fi

FROM ubuntu

RUN (cd out && ls)

RUN ls ; ls
