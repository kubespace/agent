ARG BASEIMAGE=gcr.io/distroless/static:latest
FROM $BASEIMAGE

COPY ospagent /
CMD ["/ospagent"]
