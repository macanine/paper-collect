package storage

func isRetryable(err error) bool {
	return err != nil && AsCloudError(err).Retryable
}
