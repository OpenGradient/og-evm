all: ogd-env

ogd-env: ogd-rmi
	docker build --tag cosmos/ogd -f ogd-env/Dockerfile \
		$(shell git rev-parse --show-toplevel)

ogd-rmi:
	docker rmi cosmos/ogd 2>/dev/null; true

.PHONY: all ogd-env ogd-rmi
