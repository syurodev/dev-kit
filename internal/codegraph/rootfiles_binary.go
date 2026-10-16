package codegraph

import (
	"fmt"
	"io"
	"os"
)

// BinaryFile is a file read as bytes, for a viewer that is not the text editor
// (a PDF). Signature is what a cache checks to know the bytes are still current.
type BinaryFile struct {
	Data      []byte
	Signature FileSignature
}

// FileSignature is the cheap half of "did this file change": size and mtime. A
// cache that holds the bytes compares it before reuse and reads again on a
// difference; the content hash, not this, decides what the viewer calls a
// revision.
type FileSignature struct {
	Size       int64 `json:"size"`
	ModifiedAt int64 `json:"modifiedAt"`
}

// SignatureAt stats a regular file under root without reading it.
func SignatureAt(root, relativePath string) (FileSignature, error) {
	resolved, err := resolvePath(root, relativePath)
	if err != nil {
		return FileSignature{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return FileSignature{}, fmt.Errorf("codegraph: stat file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return FileSignature{}, fmt.Errorf("codegraph: stat file: %s is not a regular file", relativePath)
	}
	return FileSignature{Size: info.Size(), ModifiedAt: info.ModTime().UnixMilli()}, nil
}

// ReadBinaryAt reads a regular file under root as bytes, up to limit. The
// folder is resolved and kept inside by the same path check as ReadFileAt; a
// file over limit is refused before it is read (ErrFileTooLarge).
func ReadBinaryAt(root, relativePath string, limit int64) (BinaryFile, error) {
	resolved, err := resolvePath(root, relativePath)
	if err != nil {
		return BinaryFile{}, err
	}
	f, err := os.Open(resolved)
	if err != nil {
		return BinaryFile{}, fmt.Errorf("codegraph: read file: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return BinaryFile{}, fmt.Errorf("codegraph: read file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return BinaryFile{}, fmt.Errorf("codegraph: read file: %s is not a regular file", relativePath)
	}
	if info.Size() > limit {
		return BinaryFile{}, fmt.Errorf("%w: %d bytes, the limit is %d", ErrFileTooLarge, info.Size(), limit)
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return BinaryFile{}, fmt.Errorf("codegraph: read file: %w", err)
	}
	if int64(len(data)) > limit {
		return BinaryFile{}, fmt.Errorf("%w: the limit is %d bytes", ErrFileTooLarge, limit)
	}
	return BinaryFile{Data: data, Signature: FileSignature{Size: int64(len(data)), ModifiedAt: info.ModTime().UnixMilli()}}, nil
}
