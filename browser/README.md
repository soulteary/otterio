# OtterIO File Browser

The browser is part of the independent Apache-2.0 OtterIO project. It is not
affiliated with or endorsed by MinIO, Inc.

## Toolchain

Use Node.js 24.21.0 or newer and Bun 1.4.2. Building the Go server additionally
requires Go 1.27.1 or newer. CI pins these toolchain versions so the committed
lockfile and embedded production assets can be verified together.

## Install, test and build

Run from `browser/`:

```sh
bun install --frozen-lockfile
bun run test --runInBand
bun run release
```

Commit `bun.lock`, `package.json` and the regenerated `production/` files
together. Go embeds these assets; updating package versions without rebuilding
them does not update the shipped console. CI rebuilds and rejects stale assets.
Use `bun update --latest` only when intentionally refreshing dependencies.

## Development server

```sh
bun run dev --host 127.0.0.1 --port 8080
```

Open `http://localhost:8080/otterio/`. Development configuration lives in
`webpack.dev.js`; production configuration lives in `webpack.prod.js`.
Configure the API proxy for your locally running OtterIO instance. Do not
expose the development server or default credentials on a public interface.

## Container toolchain

From the repository root:

```sh
docker build -f Dockerfile.dev.browser -t otterio-browser-dev .
docker run --rm -it -v "$PWD:/otterio" otterio-browser-dev bash
```

The development image includes the same Go, Node and Bun versions. It no
longer installs obsolete bindata generators: the server uses Go embedding.
