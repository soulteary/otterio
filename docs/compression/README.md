# Compression Guide

OtterIO server allows streaming compression to ensure efficient disk space usage.
Compression happens inflight, i.e objects are compressed before being written to disk(s).
OtterIO uses [`klauspost/compress/s2`](https://github.com/klauspost/compress/tree/master/s2)
streaming compression due to its stability and performance.

This algorithm is specifically optimized for machine generated content.
Write throughput is typically at least 500MB/s per CPU core,
and scales with the number of available CPU cores.
Decompression speed is typically at least 1GB/s.

This means that in cases where raw IO is below these numbers
compression will not only reduce disk usage but also help increase system throughput.
Typically, enabling compression on spinning disk systems
will increase speed when the content can be compressed.

## Get Started

### 1. Prerequisites

Install OtterIO - [OtterIO Quickstart Guide](https://github.com/soulteary/otterio#quick-start).

### 2. Run OtterIO with compression

Compression can be enabled by updating the `compress` config settings for OtterIO server config.
Config `compress` settings take extensions and mime-types to be compressed.

```bash
~ oc admin config get myotterio compression
compression extensions=".txt,.log,.csv,.json,.tar,.xml,.bin" mime_types="text/*,application/json,application/xml"
```

Default config includes most common highly compressible content extensions and mime-types.

```bash
~ oc admin config set myotterio compression extensions=".pdf" mime_types="application/pdf"
```

To show help on setting compression config values.
```bash
~ oc admin config set myotterio compression
```

To enable compression for all content, no matter the extension and content type
(except for the default excluded types) set BOTH extensions and mime types to empty.

```bash
~ oc admin config set myotterio compression enable="on" extensions="" mime_types=""
```

The compression settings may also be set through environment variables.
When set, environment variables override the defined `compress` config settings in the server config.

```bash
export OTTERIO_COMPRESS="on"
export OTTERIO_COMPRESS_EXTENSIONS=".txt,.log,.csv,.json,.tar,.xml,.bin"
export OTTERIO_COMPRESS_MIME_TYPES="text/*,application/json,application/xml"
```

### 3. Compression + Encryption

Combining encryption and compression is not safe in all setups.
This is particularly so if the compression ratio of your content reveals information about it.
See [CRIME TLS](https://en.wikipedia.org/wiki/CRIME) as an example of this.

Therefore, compression is disabled when encrypting by default, and must be enabled separately.

Evaluate compression together with your encryption and threat model before enabling it.

To enable compression+encryption use:

```bash
~ oc admin config set myotterio compression allow_encryption=on
```

Or alternatively through the environment variable `OTTERIO_COMPRESS_ALLOW_ENCRYPTION=on`.

### 4. Excluded Types

- Already compressed objects are not fit for compression since they do not have compressible patterns.
Such objects do not produce efficient [`LZ compression`](https://en.wikipedia.org/wiki/LZ77_and_LZ78)
which is a fitness factor for a lossless data compression.

Pre-compressed input typically compresses in excess of 2GiB/s per core,
so performance impact should be minimal even if precompressed data is re-compressed.
Decompressing incompressible data has no significant performance impact.

Below is a list of common files and content-types which are typically not suitable for compression.

    - Extensions

      | `gz` | (GZIP)
      | `bz2` | (BZIP2)
      | `rar` | (WinRAR)
      | `zip` | (ZIP)
      | `7z` | (7-Zip)
      | `xz` | (LZMA)
      | `mp4` | (MP4)
      | `mkv` | (MKV media)
      | `mov` | (MOV)

    - Content-Types

      | `video/*` |
      | `audio/*` |
      | `application/zip` |
      | `application/x-gzip` |
      | `application/zip` |
      | `application/x-bz2` |
      | `application/x-compress` |
      | `application/x-xz` |

All files with these extensions and mime types are excluded from compression,
even if compression is enabled for all types.

### 5. Notes

- OtterIO does not support compression for Gateway (Azure/GCS/NAS) implementations.

## To test the setup

To test this setup, practice put calls to the server using `oc` and use `mc ls` on
the data directory to view the size of the object.

## Explore Further

- [Use `oc` with OtterIO Server](https://github.com/soulteary/oc#readme)
- [Use `aws-cli` with OtterIO Server](https://github.com/soulteary/otterio/blob/main/docs/README.md)
- [Use `s3cmd` with OtterIO Server](https://github.com/soulteary/otterio/blob/main/docs/README.md)
- [Use OtterIO SDK with OtterIO Server](https://github.com/soulteary/otterio-sdk#readme)
- [OtterIO documentation](https://github.com/soulteary/otterio/blob/main/docs/README.md)
