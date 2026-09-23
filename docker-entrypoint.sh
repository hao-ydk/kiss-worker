#!/bin/sh
set -eu

data_path="${APP_DATAPATH:-data}"
case "$data_path" in
  /*) ;;
  *) data_path="/app/$data_path" ;;
esac

mkdir -p "$data_path"
chown -R kiss:kiss "$data_path"
exec su-exec kiss:kiss /app/app
