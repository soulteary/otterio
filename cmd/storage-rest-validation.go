/*
 * OtterIO Cloud Storage, (C) 2026 soulteary, https://github.com/soulteary/otterio
 * Licensed under the Apache License, Version 2.0.
 */

package cmd

import (
	"encoding/binary"
	"io"
	"net/http"
)

// These limits apply to buffered internal RPCs, NOT S3 object sizes. Large
// objects use CreateFile/ReadFileStream. Keep one request's allocations bounded.
const (
	maxStorageRESTBuffer        = int64(64 << 20)
	maxStorageRESTVersions      = 1000
	maxStorageMsgpackCollection = 10000
	maxStorageMsgpackValues     = 1000000
	maxStorageMsgpackDepth      = 64
)

func validStorageRESTBufferSize(n int64) bool { return n >= 0 && n <= maxStorageRESTBuffer }
func validStorageRESTVersionCount(n int) bool { return n >= 0 && n <= maxStorageRESTVersions }

// Read actual bytes, not an allocation derived from an untrusted declaration.
// Unknown-length batch bodies remain supported, but are bounded identically.
func readStorageRESTBody(r *http.Request) ([]byte, error) {
	if r.ContentLength < -1 || r.ContentLength > maxStorageRESTBuffer {
		return nil, errInvalidArgument
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, maxStorageRESTBuffer+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > maxStorageRESTBuffer {
		return nil, errInvalidArgument
	}
	if r.ContentLength >= 0 && int64(len(b)) != r.ContentLength {
		return nil, io.ErrUnexpectedEOF
	}
	return b, nil
}

// Preflight MessagePack without allocating collections. A body byte limit
// alone cannot stop generated decoders allocating from a tiny array32/map32
// header claiming billions of entries. Depth, collection size and total value
// count are checked before invoking FileInfo.UnmarshalMsg.
func checkStorageMsgpack(b []byte, depth int, budget *int) ([]byte, error) {
	if depth > maxStorageMsgpackDepth || len(b) == 0 || *budget <= 0 {
		return nil, errInvalidArgument
	}
	*budget--
	tag := b[0]
	b = b[1:]
	var payload, children uint64
	var lengthBytes int
	var collection, mapping, extension bool
	switch {
	case tag <= 0x7f || tag >= 0xe0 || tag == 0xc0 || tag == 0xc2 || tag == 0xc3:
	case tag >= 0xa0 && tag <= 0xbf:
		payload = uint64(tag & 31)
	case tag >= 0x90 && tag <= 0x9f:
		collection = true
		children = uint64(tag & 15)
	case tag >= 0x80 && tag <= 0x8f:
		collection = true
		mapping = true
		children = uint64(tag & 15)
	default:
		switch tag {
		case 0xc4, 0xd9:
			lengthBytes = 1
		case 0xc5, 0xda:
			lengthBytes = 2
		case 0xc6, 0xdb:
			lengthBytes = 4
		case 0xc7:
			lengthBytes = 1
			extension = true
		case 0xc8:
			lengthBytes = 2
			extension = true
		case 0xc9:
			lengthBytes = 4
			extension = true
		case 0xca, 0xce, 0xd2:
			payload = 4
		case 0xcb, 0xcf, 0xd3:
			payload = 8
		case 0xcc, 0xd0:
			payload = 1
		case 0xcd, 0xd1:
			payload = 2
		case 0xd4:
			payload = 2
		case 0xd5:
			payload = 3
		case 0xd6:
			payload = 5
		case 0xd7:
			payload = 9
		case 0xd8:
			payload = 17
		case 0xdc:
			lengthBytes = 2
			collection = true
		case 0xdd:
			lengthBytes = 4
			collection = true
		case 0xde:
			lengthBytes = 2
			collection = true
			mapping = true
		case 0xdf:
			lengthBytes = 4
			collection = true
			mapping = true
		default:
			return nil, errInvalidArgument
		}
	}
	if lengthBytes != 0 {
		if len(b) < lengthBytes {
			return nil, io.ErrUnexpectedEOF
		}
		switch lengthBytes {
		case 1:
			payload = uint64(b[0])
		case 2:
			payload = uint64(binary.BigEndian.Uint16(b))
		case 4:
			payload = uint64(binary.BigEndian.Uint32(b))
		}
		b = b[lengthBytes:]
		if collection {
			children = payload
			payload = 0
		}
	}
	if collection {
		if children > maxStorageMsgpackCollection {
			return nil, errInvalidArgument
		}
		if mapping {
			children *= 2
		}
		if children > uint64(len(b)) || children > uint64(*budget) {
			return nil, errInvalidArgument
		}
		for ; children > 0; children-- {
			var err error
			b, err = checkStorageMsgpack(b, depth+1, budget)
			if err != nil {
				return nil, err
			}
		}
		return b, nil
	}
	if extension {
		payload++
	} // extension type byte
	if payload > uint64(len(b)) {
		return nil, io.ErrUnexpectedEOF
	}
	return b[int(payload):], nil
}

func decodeStorageFileInfos(r *http.Request, count int) ([]FileInfo, error) {
	if !validStorageRESTVersionCount(count) {
		return nil, errInvalidArgument
	}
	b, err := readStorageRESTBody(r)
	if err != nil {
		return nil, err
	}
	rest := b
	budget := maxStorageMsgpackValues
	for i := 0; i < count; i++ {
		rest, err = checkStorageMsgpack(rest, 0, &budget)
		if err != nil {
			return nil, err
		}
	}
	if len(rest) != 0 {
		return nil, errInvalidArgument
	}
	infos := make([]FileInfo, count)
	for i := range infos {
		b, err = infos[i].UnmarshalMsg(b)
		if err != nil {
			return nil, err
		}
	}
	if len(b) != 0 {
		return nil, errInvalidArgument
	}
	return infos, nil
}

func decodeStorageFileInfo(r *http.Request, fi *FileInfo) error {
	infos, err := decodeStorageFileInfos(r, 1)
	if err != nil {
		return err
	}
	*fi = infos[0]
	return nil
}
