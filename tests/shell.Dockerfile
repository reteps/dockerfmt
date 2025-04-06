# https://github.com/jessfraz/dockfmt/issues/2
# set up PrairieLearn and run migrations to initialize the DB
RUN chmod +x /PrairieLearn/scripts/init.sh \
    && mkdir /course{,{2..9}} \
    && mkdir -p /workspace_{main,host}_zips \
    && mkdir -p /jobs \
    # Here is a comment in the middle of my command
    && /PrairieLearn/scripts/start_postgres.sh \
                && cd /PrairieLearn \               
    && make build \
    && node apps/prairielearn/dist/server.js --migrate-and-exit \            
    && su postgres -c "createuser -s root" \
    && /PrairieLearn/scripts/start_postgres.sh stop \
    && /PrairieLearn/scripts/gen_ssl.sh \
    && git config --global user.email "dev@example.com" \
    && git config --global user.name "Dev User" \
    && git config --global safe.directory '*'



healthcheck --interval=5m --timeout=3s \
  CMD curl -f http://localhost/ || exit 1
CMD /PrairieLearn/scripts/init.sh
