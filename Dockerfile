ARG BASEIMAGE=registry.cn-hangzhou.aliyuncs.com/kubespace/distroless-static:latest
FROM $BASEIMAGE

COPY agent /agent
CMD ["/agent"]
