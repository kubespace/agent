export GOPROXY=https://proxy.golang.com.cn,direct
CGO_ENABLED=0 LD_FLAGS=-s GOOS=linux go build -o agent
