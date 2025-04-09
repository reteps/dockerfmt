MAINTAINER me

# This copies in all the `package.json` files in `apps` and `packages`, which
                # Yarn needs to correctly install all dependencies in our workspaces.
# The `--parents` flag is used to preserve parent directories for the sources.
#
# We also need to copy both the `.yarn` directory and the `.yarnrc.yml` file,
# both of which are necessary for Yarn to correctly install dependencies.
#
# Finally, we copy `packages/bind-mount/` since this package contains native
# code that will be built during the install process.
COPY --parents .yarn/ yarn.lock .yarnrc.yml **/package.json packages/bind-mount/ /bar/

# Bla Bla
RUN cd /bar && yarn dlx node-gyp install && yarn install --immutable --inline-builds && yarn cache clean







# NOTE: Modify .dockerignore to allowlist files/directories to copy.
COPY .          /bar/


# ....