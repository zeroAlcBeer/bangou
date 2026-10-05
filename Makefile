REGISTRY_URL ?= 192.168.0.110:5000
TAG ?= latest
PLATFORM ?= linux/arm64
API_IMAGE ?= $(REGISTRY_URL)/bangou-api:$(TAG)
WEB_IMAGE ?= $(REGISTRY_URL)/bangou-web:$(TAG)

# Immutable tag derived from git HEAD.
GIT_SHA := $(shell git rev-parse --short=12 HEAD)

.PHONY: help test build run dev docker-build docker-push release release-sha compose-up compose-down clean

help:
	@echo "Targets:"
	@echo "  make test          - Run Go tests"
	@echo "  make build         - Build Go binary"
	@echo "  make run           - Run Go API server locally"
	@echo "  make dev           - Start Go API + Vite dev server"
	@echo "  make docker-build  - Build Docker images (API + Web)"
	@echo "  make docker-push   - Push Docker images"
	@echo "  make release       - Build + push both images (:git-sha and :latest)"
	@echo "  make release-sha   - Alias for release"
	@echo "  make compose-up    - Start services with docker compose"
	@echo "  make compose-down  - Stop services"
	@echo "  make clean         - Remove build artifacts"
	@echo ""
	@echo "Pi deployment flow:"
	@echo "  local:  make release"
	@echo "  on pi:  cd /opt/stacks/bangou"
	@echo "          sudo sed -i -E 's#(bangou-(api|web)):[A-Za-z0-9._-]+#\1:<sha>#' compose.yaml"
	@echo "          sudo docker compose up -d"
	@echo "  verify: curl -s http://192.168.0.110:8890/api/healthz | jq .sha"

test:
	go test ./...

build:
	go build -o bangou .

run:
	go run . --db ./bangou.db --listen :8080

dev:
	@echo "Go API on :8080, Vite on :5173 (proxy /api -> :8080)"
	@echo "Open http://localhost:5173"
	@go run . --db ./bangou.db --listen :8080 & \
	cd frontend && npm run dev

docker-build:
	DOCKER_BUILDKIT=1 docker build --platform $(PLATFORM) --build-arg BUILD_SHA=$(GIT_SHA) -t $(API_IMAGE) -f Dockerfile .
	DOCKER_BUILDKIT=1 docker build --platform $(PLATFORM) -t $(WEB_IMAGE) -f Dockerfile.frontend .

docker-push:
	docker push $(API_IMAGE)
	docker push $(WEB_IMAGE)

# Build and push both images, tagged :$(GIT_SHA) + :latest. Deploys are manual
# — after releasing, pin the SHA in the Pi stack compose and recreate (see
# `make help` → "Pi deployment flow").
release:
	@if ! git diff-index --quiet HEAD --; then \
		echo "Warning: working tree dirty — :$(GIT_SHA) will NOT represent committed state"; \
	fi
	$(MAKE) docker-build TAG=$(GIT_SHA)
	docker tag  $(REGISTRY_URL)/bangou-api:$(GIT_SHA) $(REGISTRY_URL)/bangou-api:latest
	docker tag  $(REGISTRY_URL)/bangou-web:$(GIT_SHA) $(REGISTRY_URL)/bangou-web:latest
	docker push $(REGISTRY_URL)/bangou-api:$(GIT_SHA)
	docker push $(REGISTRY_URL)/bangou-api:latest
	docker push $(REGISTRY_URL)/bangou-web:$(GIT_SHA)
	docker push $(REGISTRY_URL)/bangou-web:latest
	@echo "--> Next on pi: cd /opt/stacks/bangou &&"
	@echo "    sudo sed -i -E 's#(bangou-(api|web)):[A-Za-z0-9._-]+#\1:$(GIT_SHA)#' compose.yaml &&"
	@echo "    sudo docker compose up -d"
	@echo "--> Verify: curl -s http://192.168.0.110:8890/api/healthz | jq .sha  →  $(GIT_SHA)"

release-sha: release

compose-up:
	API_IMAGE=$(API_IMAGE) WEB_IMAGE=$(WEB_IMAGE) docker compose up -d --build

compose-down:
	docker compose down

clean:
	rm -f bangou
	rm -rf frontend/dist frontend/node_modules
