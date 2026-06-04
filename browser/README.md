# OtterIO File Browser

``OtterIO Browser`` provides minimal set of UI to manage buckets and objects on ``otterio`` server. ``OtterIO Browser`` is written in javascript and released under the [Apache 2.0 License](../LICENSE).

> NOTE: This is part of OtterIO, an independent, community-maintained fork of OtterIO
> (https://github.com/soulteary/otterio). It is not affiliated with, endorsed by,
> or sponsored by MinIO, Inc. "MinIO" is a trademark of MinIO, Inc.


## Installation

### Install bun
```sh
curl -fsSL https://bun.sh/install | bash
exec -l $SHELL
```

### Install dependencies
```sh
bun install
```

## Generating Assets

```sh
bun run release
```

This generates `production` in the current directory. 


## Run OtterIO Browser with live reload

### Run OtterIO Browser with live reload

```sh
bun run dev
```

Open [http://localhost:8080/otterio/](http://localhost:8080/otterio/) in your browser to play with the application.

### Run OtterIO Browser with live reload on custom port

Edit `browser/webpack.config.js`

```diff
diff --git a/browser/webpack.config.js b/browser/webpack.config.js
index 3ccdaba..9496c56 100644
--- a/browser/webpack.config.js
+++ b/browser/webpack.config.js
@@ -58,6 +58,7 @@ var exports = {
     historyApiFallback: {
       index: '/otterio/'
     },
+    port: 8888,
     proxy: {
       '/otterio/webrpc': {
         target: 'http://localhost:9000',
@@ -97,7 +98,7 @@ var exports = {
 if (process.env.NODE_ENV === 'dev') {
   exports.entry = [
     'webpack/hot/dev-server',
-    'webpack-dev-server/client?http://localhost:8080',
+    'webpack-dev-server/client?http://localhost:8888',
     path.resolve(__dirname, 'app/index.js')
   ]
 }
```

```sh
bun run dev
```

Open [http://localhost:8888/otterio/](http://localhost:8888/otterio/) in your browser to play with the application.

### Run OtterIO Browser with live reload on any IP

Edit `browser/webpack.config.js`

```diff
diff --git a/browser/webpack.config.js b/browser/webpack.config.js
index 8bdbba53..139f6049 100644
--- a/browser/webpack.config.js
+++ b/browser/webpack.config.js
@@ -71,6 +71,7 @@ var exports = {
     historyApiFallback: {
       index: '/otterio/'
     },
+    host: '0.0.0.0',
     proxy: {
       '/otterio/webrpc': {
         target: 'http://localhost:9000',
```

```sh
bun run dev
```

Open [http://IP:8080/otterio/](http://IP:8080/otterio/) in your browser to play with the application.


## Run tests

    bun run test


## Docker development environment

This approach will download the sources on your machine such that you are able to use your IDE or editor of choice.
A Docker container will be used in order to provide a controlled build environment without messing with your host system.

### Prepare host system

Install [Git](https://git-scm.com/book/en/v2/Getting-Started-Installing-Git) and [Docker](https://docs.docker.com/get-docker/).

### Development within container

Prepare and build container
```
git clone git@github.com:soulteary/otterio.git
cd otterio
docker build -t otterio-dev -f Dockerfile.dev.browser .
```

Run container, build and run core
```sh
docker run -it --rm --name otterio-dev -v "$PWD":/otterio otterio-dev

cd /otterio/browser
bun install
bun run release
cd /otterio
make
./otterio server /data
```
Note `Endpoint` IP (the one which is _not_ `127.0.0.1`), `AccessKey` and `SecretKey` (both default to `otterioadmin`) in order to enter them in the browser later.


Open another terminal.
Connect to container
```sh
docker exec -it otterio-dev bash
```

Apply patch to allow access from outside container
```sh
cd /otterio
git apply --ignore-whitespace <<EOF
diff --git a/browser/webpack.config.js b/browser/webpack.config.js
index 8bdbba53..139f6049 100644
--- a/browser/webpack.config.js
+++ b/browser/webpack.config.js
@@ -71,6 +71,7 @@ var exports = {
     historyApiFallback: {
       index: '/otterio/'
     },
+    host: '0.0.0.0',
     proxy: {
       '/otterio/webrpc': {
         target: 'http://localhost:9000',
EOF
```

Build and run frontend with auto-reload
```sh
cd /otterio/browser
bun install
bun run dev
```

Open [http://IP:8080/otterio/](http://IP:8080/otterio/) in your browser to play with the application.

