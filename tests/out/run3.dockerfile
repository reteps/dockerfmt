RUN apt-get update \
    # Comment here
    && apt-get install -y --no-install-recommends \
        # Another comment here
        man-db unminimize \
        # Multiline comment here
        # Here
        # And here
        gosu curl git htop less nano unzip vim wget zip \
    && yes | unminimize \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/* \
    && find /tmp -not -path /tmp -delete
