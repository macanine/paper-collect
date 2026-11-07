package jobexec

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
)

// ErrContentMismatch means streamed content did not match its declared size or hash.
var ErrContentMismatch = errors.New("原件内容与记录的大小或 SHA-256 不一致")

// VerifyReader returns a reader that fails at EOF unless exactly size bytes
// with the given SHA-256 were read. Callers must read to EOF before trusting
// what they consumed.
func VerifyReader(r io.ReadCloser, sha string, size int64) io.ReadCloser {
	return &verifyingReader{r: r, h: sha256.New(), sha: sha, size: size}
}

type verifyingReader struct {
	r    io.ReadCloser
	h    hash.Hash
	sha  string
	size int64
	n    int64
}

func (v *verifyingReader) Read(p []byte) (int, error) {
	n, err := v.r.Read(p)
	v.n += int64(n)
	v.h.Write(p[:n])
	if v.n > v.size {
		return n, ErrContentMismatch
	}
	if errors.Is(err, io.EOF) && (v.n != v.size || hex.EncodeToString(v.h.Sum(nil)) != v.sha) {
		return n, ErrContentMismatch
	}
	return n, err
}

func (v *verifyingReader) Close() error { return v.r.Close() }
