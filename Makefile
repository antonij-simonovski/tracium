# Tracium — common tasks. Run `make help`.
.DEFAULT_GOAL := help
SPEC := spec/pricing/pricing.json
CHART_FILES := deploy/helm/tracium/files

help: ## List targets
	@grep -hE '^[a-z-]+:.*##' $(MAKEFILE_LIST) | sed 's/:.*##/\t/' | sort

test: ## Run all unit tests (go + dashboard)
	set -e; for mod in $$(find collector api -name go.mod -not -path '*/_build/*'); do (cd $$(dirname $$mod) && go test ./...); done
	cd dashboard && npm ci --no-audit && npm test

build-collector: ## Assemble the collector binary via OCB (GOWORK off — OCB owns its module graph)
	cd collector && GOWORK=off go run go.opentelemetry.io/collector/cmd/builder@v0.116.0 --config builder/oss.builder.yaml

sync-generated: ## Copy the source-of-truth pricing + schema into the Helm chart's files/
	mkdir -p $(CHART_FILES)/pricing $(CHART_FILES)/schema
	cp $(SPEC) $(CHART_FILES)/pricing/pricing.json
	cp collector/schema/*.sql collector/schema/*.sh $(CHART_FILES)/schema/

up: ## Build + start the full stack from source, databases exposed on localhost (needs .env)
	docker compose -f docker-compose.yml -f compose.build.yaml -f compose.dev.yaml up --build

down: ## Stop the stack
	docker compose -f docker-compose.yml -f compose.build.yaml -f compose.dev.yaml down

helm-lint: sync-generated ## Lint the Helm chart
	helm lint deploy/helm/tracium

smoke: ## Build and verify a fresh isolated stack (Docker + Python 3)
	bash deploy/tests/smoke.sh
