package service

import "github.com/nic/paper-collect/backend/internal/jobexec"

// redact removes URLs, credentials and known secret values, then truncates.
func redact(msg string, secrets ...string) string { return jobexec.Redact(msg, secrets...) }

// storageErrorText turns an adapter error into a stable, storable message.
func storageErrorText(err error, secrets ...string) string {
	return jobexec.StorageErrorText(err, secrets...)
}

// publicMessage converts a job failure into text that is safe to store and display.
func publicMessage(err error) string { return jobexec.PublicMessage(err) }
