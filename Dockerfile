# GoReleaser builds the binary; this image only packages it.
#
# static-debian12 rather than scratch: the CLI wraps MCP servers, which are
# usually npx/python processes, so a shell-less scratch image would be useless for
# the main use case. Still no package manager and no libc to keep patched.
FROM gcr.io/distroless/static-debian12:nonroot

COPY gremlyn /usr/local/bin/gremlyn

USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/gremlyn"]
