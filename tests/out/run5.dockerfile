# https://github.com/un-ts/prettier/issues/441#issuecomment-2793674631
FROM ghcr.io/zerocluster/node/app

RUN \
    # install dependencies
    NODE_ENV=production npm install-clean \
    # cleanup
    && /usr/bin/env bash <(curl -fsSL https://raw.githubusercontent.com/softvisio/scripts/main/env-build-node.sh) cleanup

RUN \
    # install dependencies
    # multiline comment
        NODE_ENV=production npm install-clean \
    # cleanup
    && /usr/bin/env bash <(curl -fsSL https://raw.githubusercontent.com/softvisio/scripts/main/env-build-node.sh) cleanup
