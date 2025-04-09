RUN apt-get update \
    # Comment here
    && apt-get install -y --no-install-recommends \
        # Another comment here
        man-db unminimize \
                    # Multiline comment here
        # Here
        gosu curl git htop less nano unzip vim wget zip && \
    yes | unminimize && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/* \
    && find /tmp -not -path /tmp -delete

RUN apt-get update && \
    # Run 'unminimize' to add docs
    apt-get install -y --no-install-recommends man-db unminimize \
    && yes | unminimize \
    && apt-get install -y --no-install-recommends \
    # Reverse proxy workaround for PrairieLearn:
    nginx \
    gettext \
    gosu \
    fonts-dejavu \
    # Utilities for convenience debugging this container:
    less htop vim nano silversearcher-ag zip unzip git cmake curl wget sqlite3 && \
    # Test:
    gosu nobody true && \
    # Cleanup:
    apt-get clean \
    && rm -rf /var/lib/apt/lists/* \
    && find /tmp -not -path /tmp -delete

RUN apt-get update && \
    apt-get install -y --no-install-recommends man-db unminimize \
    && yes | unminimize \
    && apt-get install -y --no-install-recommends \
    nginx \
    gettext \
    gosu \
    fonts-dejavu \
    less htop vim nano silversearcher-ag zip unzip git cmake curl wget sqlite3 && \
    gosu nobody true && \
    apt-get clean \
    && rm -rf /var/lib/apt/lists/* \
    && find /tmp -not -path /tmp -delete
