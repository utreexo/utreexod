help: ## Display help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

install-rust: ## Install rust if not installed
	if ! type cargo; then curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh ; fi

install-uniffi-bindgen-go: install-rust ## Install uniffi-bindgen-go if not installed
	if ! type uniffi-bindgen-go; then cargo install uniffi-bindgen-go --git https://github.com/NordSecurity/uniffi-bindgen-go --tag v0.2.0+v0.25.0 ; fi

build-bdk: install-uniffi-bindgen-go ## Build BDK static library
	uniffi-bindgen-go -o bdkwallet bdkwallet/bdkgo_crate/src/bdkgo.udl
	cargo build --release

build-utreexod: build-bdk ## Build utreexod with all features
	go build --tags=bdkwallet -o . ./...

build-utreexod-without-bdk: ## Build utreexod without BDK wallet
	go build -o . ./...

test: build-bdk ## Run all test
	cargo test
	sh ./goclean.sh

all: build-bdk build-utreexod ## Build all

clean: ## Clean all
	rm utreexod
	go clean
	cargo clean
