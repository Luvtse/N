.PHONY: setup dev test deploy clean

setup:
	chmod +x scripts/*.sh
	./scripts/setup.sh

dev:
	cd docker && docker-compose up -d

test:
	cd backend && go test ./... -race -cover
	cd frontend && flutter test
	cd driver-app && flutter test

deploy:
	./scripts/deploy.sh

clean:
	docker-compose down -v
	terraform destroy -auto-approve