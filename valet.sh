#!/usr/bin/env bash
##
#   Copyright 2025 TechDivision GmbH
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at
#
#       http://www.apache.org/licenses/LICENSE-2.0
#
#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.
##

# Minimal shim that delegates to the Go CLI binary.
# The actual valet.sh CLI has been reimplemented in Go.

set -e

VALET_GO_CLI="/usr/local/valet-sh/bin/valet"

if [ ! -x "$VALET_GO_CLI" ]; then
    echo "Error: valet CLI binary not found at $VALET_GO_CLI" >&2
    echo "Please re-run the installer to set up valet.sh." >&2
    exit 255
fi

exec "$VALET_GO_CLI" "$@"
