ARG BASEIMAGE=kubespace/distroless-static:latest
FROM $BASEIMAGE

COPY agent /agent
CMD ["/agent"]
