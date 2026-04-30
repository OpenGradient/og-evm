#!/usr/bin/env sh
set -x

BINARY=/evmd/${BINARY:-evmd}
ID=${ID:-0}
LOG=${LOG:-evmd.log}

if ! [ -f "${BINARY}" ]; then
	echo "The binary $(basename "${BINARY}") cannot be found. Please add the binary to the shared folder. Please use the BINARY environment variable if the name of the binary is not 'evmd'"
	exit 1
fi

export EVMDHOME="/data/node${ID}/evmd"

# Enable Prometheus metrics in CometBFT config
CONFIG_TOML="${EVMDHOME}/config/config.toml"
if [ -f "${CONFIG_TOML}" ]; then
  sed -i 's/prometheus = false/prometheus = true/' "${CONFIG_TOML}"
fi

# Enable SDK telemetry and set OTel instance ID
APP_TOML="${EVMDHOME}/config/app.toml"
if [ -f "${APP_TOML}" ]; then
  sed -i '/^\[telemetry\]/,/^\[/ s/enabled = false/enabled = true/' "${APP_TOML}"
  sed -i "s/instance-id = \"\"/instance-id = \"validator-${ID}\"/" "${APP_TOML}"
fi

if [ -d "$(dirname "${EVMDHOME}"/"${LOG}")" ]; then
  "${BINARY}" --home "${EVMDHOME}" "$@" | tee "${EVMDHOME}/${LOG}"
else
  "${BINARY}" --home "${EVMDHOME}" "$@"
fi
