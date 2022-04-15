ARG BASEIMAGE=gcr.io/distroless/static:latest
FROM $BASEIMAGE

COPY agent /
CMD ["/agent"]
