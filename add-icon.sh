#!/bin/bash
iconset="${1%%:*}"
icon="${1#*:}"

BASE_DIR="./assets/icons"

mkdir -p "$BASE_DIR/$iconset"
wget -4 -O "$BASE_DIR/$iconset/$icon.svg" "https://api.iconify.design/$iconset/$icon.svg"
sed -i -E \
	-e 's/<svg/<svg id="icon"/' \
	-e 's/width="[^"]+"/width="100%"/g' \
	-e 's/height="[^"]+"/height="100%"/g' \
	"$BASE_DIR/$iconset/$icon.svg"
