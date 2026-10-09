#!/bin/bash -e
#
#  Mint (C) 2017-2020 Minio, Inc.
#
#  Licensed under the Apache License, Version 2.0 (the "License");
#  you may not use this file except in compliance with the License.
#  You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
#  Unless required by applicable law or agreed to in writing, software
#  distributed under the License is distributed on an "AS IS" BASIS,
#  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#  See the License for the specific language governing permissions and
#  limitations under the License.
#

export APT="apt --quiet --yes"
export WGET="wget --quiet --no-check-certificate"

# install nodejs source list
if ! $WGET --output-document=- https://deb.nodesource.com/setup_24.x | bash -; then
    echo "unable to set nodejs repository"
    exit 1
fi

$APT install apt-transport-https

# .NET 10 is provided by the Ubuntu 24.04 package feed.
$APT update
$APT install gnupg ca-certificates

# download and install golang
GO_VERSION="1.27.2"
GO_INSTALL_PATH="/usr/local"
download_url="https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz"
go_archive=$(mktemp)
if ! $WGET --tries=3 --output-document="$go_archive" "$download_url" ||
    ! tar -C "${GO_INSTALL_PATH}" -zxf "$go_archive"; then
    rm -f "$go_archive"
    echo "unable to install go$GO_VERSION"
    exit 1
fi
rm -f "$go_archive"

xargs --arg-file="${MINT_ROOT_DIR}/install-packages.list" apt --quiet --yes install

# Keep pip installs outside the distribution-managed Python environment.
python3 -m venv /opt/mint-venv

sync
