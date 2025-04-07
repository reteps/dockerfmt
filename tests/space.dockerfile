    FROM foobar   
    RUN ls
    LABEL foo=bar   
    HEALTHCHECK NONE   
    CMD ls
    COPY . .   
    ADD . .   
