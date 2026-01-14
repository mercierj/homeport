package middleware

// ClearCredentials removes all credentials (storage and database) for a session token.
// This should be called when a user logs out to prevent credential leakage.
func (s *CredentialStore) ClearCredentials(token string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.storage, token)
	delete(s.database, token)
}
