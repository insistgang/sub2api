package service

// AuthenticatedAPIKeyOwnerRole reads the hydrated owner role only when the key,
// owner and authenticated user identities agree. Unknown identities stay limited.
func AuthenticatedAPIKeyOwnerRole(apiKey *APIKey, userID int64) string {
	if apiKey == nil || apiKey.User == nil || userID <= 0 || apiKey.UserID != userID || apiKey.User.ID != userID {
		return ""
	}
	return apiKey.User.Role
}
