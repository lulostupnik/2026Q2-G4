TF_DIR := terraform/organization
TF     ?= terraform
REGION ?= us-east-1
# Hash del árbol de apps/ledger: cambia sólo si cambia el backend, no con cada commit.
IMAGE_TAG ?= $(shell git rev-parse --short=12 HEAD:apps/ledger)
PLATFORMS ?= linux/amd64,linux/arm64
# Prefijo de todo: los nombres de los recursos (project_name) y los de los buckets que crea
# scripts/buckets.sh salen de acá, así no se desalinean.
PROJECT ?= ledger

export REGION
export PROJECT
export TF_VAR_image_tag = $(IMAGE_TAG)
export TF_VAR_project_name = $(PROJECT)

.PHONY: help fmt fmt-check init validate tag imagen plan apply destroy clean

help:
	@echo "make fmt        Formatea todo terraform/"
	@echo "make fmt-check  Verifica el formato sin modificar (lo mismo que corre el pipeline)"
	@echo "make init       Crea el bucket del state si no existe e inicializa terraform/organization"
	@echo "make validate   init sin backend + validate (no necesita AWS)"
	@echo "make tag        Muestra el tag de la imagen: el hash de apps/ledger en el commit actual"
	@echo "make imagen     Construye la imagen de apps/ledger (amd64 y arm64) y la sube a ECR con ese tag"
	@echo "make plan       Genera el plan en terraform/organization/tfplan"
	@echo "make apply      Crea los buckets del proyecto si no existen y aplica el plan guardado"
	@echo "make destroy    Destruye toda la infraestructura y después los buckets (proyecto y state)"
	@echo "make clean      Borra .terraform/ y tfplan"

fmt:
	$(TF) fmt -recursive terraform

fmt-check:
	$(TF) fmt -check -recursive terraform

init:
	@bucket=$$(scripts/buckets.sh state) && \
	$(TF) -chdir=$(TF_DIR) init -backend-config="bucket=$$bucket"

validate:
	$(TF) -chdir=$(TF_DIR) init -backend=false
	$(TF) -chdir=$(TF_DIR) validate

tag:
	@echo $(IMAGE_TAG)

imagen: init
	@test -z "$$(git status --porcelain -- apps/ledger)" || { echo "apps/ledger tiene cambios sin commitear: la imagen no seria la del tag $(IMAGE_TAG)" >&2; exit 1; }
	$(TF) -chdir=$(TF_DIR) apply -auto-approve -compact-warnings -target=aws_ecr_repository.ledger
	@repo=$$($(TF) -chdir=$(TF_DIR) output -raw ecr_repository_url) && \
	if aws ecr describe-images --region $(REGION) --repository-name "$${repo#*/}" --image-ids imageTag=$(IMAGE_TAG) >/dev/null 2>&1; then \
	  echo "La imagen $(IMAGE_TAG) ya esta en ECR"; \
	else \
	  aws ecr get-login-password --region $(REGION) | docker login --username AWS --password-stdin "$${repo%%/*}" && \
	  { docker buildx inspect ledger >/dev/null 2>&1 || docker buildx create --name ledger --driver docker-container >/dev/null; } && \
	  docker buildx build --builder ledger --platform $(PLATFORMS) --provenance=false \
	    --build-arg VERSION=$(IMAGE_TAG) -t "$$repo:$(IMAGE_TAG)" --push apps/ledger; \
	fi

plan: init
	$(TF) -chdir=$(TF_DIR) plan -out=tfplan

apply:
	scripts/buckets.sh create
	$(TF) -chdir=$(TF_DIR) apply tfplan

destroy: init
	$(TF) -chdir=$(TF_DIR) destroy $(DESTROY_FLAGS)
	@if [ -n "$$($(TF) -chdir=$(TF_DIR) state list)" ]; then \
	  echo "Quedan recursos en el state: no se borran los buckets" >&2; exit 1; \
	fi
	scripts/buckets.sh delete

clean:
	rm -rf $(TF_DIR)/.terraform $(TF_DIR)/tfplan
