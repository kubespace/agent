ENVVAR=CGO_ENABLED=0 LD_FLAGS=-s
GOOS?=linux
REGISTRY?=openspacee
TAG?=dev


build-binary: clean
	$(ENVVAR) GOOS=$(GOOS) go build -o ospagent

clean:
	rm -f ospagent

docker-builder:
	docker images | grep ospagent-builder || docker build -t ospagent-builder ./builder

build-in-docker: clean docker-builder
	docker run -v `pwd`:/gopath/src/github.com/kubespace/agent/ ospagent-builder:latest bash -c 'cd /gopath/src/github.com/kubespace/agent && make build-binary'

make-image: build-in-docker
	docker build -t ${REGISTRY}/ospagent:${TAG} .

push-image:
	docker push ${REGISTRY}/ospagent:${TAG}
	docker tag ${REGISTRY}/ospagent:${TAG} ${REGISTRY}/ospagent:latest
	docker push ${REGISTRY}/ospagent:latest

execute-release: make-image push-image

# make build-in-docker
# TAG=v1.2.0 make execute-release