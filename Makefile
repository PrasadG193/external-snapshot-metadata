GOOS ?= linux
GOARCH ?= amd64
# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif


IMAGE_REPO_SERVER ?= prasadg193/external-snapshot-metadata
IMAGE_TAG_SERVER ?= latest
IMAGE_REPO_CLIENT ?= prasadg193/sample-cbt-client
IMAGE_TAG_CLIENT ?= latest

.PHONY: proto
proto:
	protoc -I=proto \
		--go_out=pkg/grpc --go_opt=paths=source_relative \
   	--go-grpc_out=pkg/grpc --go-grpc_opt=paths=source_relative \
		proto/*.proto


.PHONY: build
build:
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build  -o grpc-server ./cmd/server/main.go
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build  -o grpc-client ./cmd/client/main.go

image: build
	docker build --platform=linux/amd64 -t $(IMAGE_REPO_SERVER):$(IMAGE_TAG_SERVER) -f Dockerfile-grpc .
	docker build --platform=linux/amd64 -t $(IMAGE_REPO_CLIENT):$(IMAGE_TAG_CLIENT) -f Dockerfile-grpc-client .

push:
	docker push $(IMAGE_REPO_SERVER):$(IMAGE_TAG_SERVER)
	docker push $(IMAGE_REPO_CLIENT):$(IMAGE_TAG_CLIENT)

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) crd paths="./pkg/..." output:crd:artifacts:config=deploy/crd


.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./pkg/..."

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary. If wrong version is installed, it will be overwritten.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen && $(LOCALBIN)/controller-gen --version | grep -q $(CONTROLLER_TOOLS_VERSION) || \
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

##@ Build Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
