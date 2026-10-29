package service

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
)

// Each received ZIP has an independent durable staging file: the upload entry
// and content hash make concurrent submissions unable to replace one another.
// It is separate from the content-addressed store used by archived materials.
func (s *Service) archiveUploadPath(entryID, contentSHA string) string {
	key := sha256.Sum256([]byte(entryID + "\x00" + contentSHA))
	return filepath.Join(s.blobs.DataDir(), "tmp", "upload-archives", fmt.Sprintf("%x.zip", key))
}

func (s *Service) stageArchive(tmpPath, entryID, contentSHA string) error {
	dest := s.archiveUploadPath(entryID, contentSHA)
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	return os.Rename(tmpPath, dest)
}
