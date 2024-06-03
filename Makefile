# Image URL to use all building/pushing image targets
IMG ?= ghcr.io/souleb/controller
TAG ?= v1.0.3

# Allows for defining additional Docker buildx arguments,
# e.g. '--push'.
BUILD_ARGS ?=

docker-build:  ## Build the Docker image
	docker buildx build \
		-t $(IMG):$(TAG) \
		$(BUILD_ARGS) .


deploy:  ## Deploy controller in the configured Kubernetes cluster in ~/.kube/config
	cd config/manager && kustomize edit set image ghcr.io/souleb/controller=$(IMG):$(TAG)
	kustomize build config/manager | kubectl apply -f -
